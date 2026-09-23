/*
 * pf-aliasd.c - Lightweight C Companion Daemon for FreeBSD / OPNsense
 * Uses /dev/pf with libpfctl and listens on Unix SOCK_SEQPACKET.
 *
 * Compilation on FreeBSD/OPNsense:
 *   cc -O2 -Wall -o pf-aliasd pf-aliasd.c -lpfctl
 */

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <signal.h>
#include <fcntl.h>
#include <errno.h>
#include <time.h>
#include <sys/types.h>
#include <sys/socket.h>
#include <sys/un.h>
#include <sys/stat.h>
#include <netinet/in.h>
#include <arpa/inet.h>

#ifdef __FreeBSD__
#include <net/if.h>
#include <netpfil/pf/pf.h>
#include <libpfctl.h>
#endif

#define DEFAULT_SOCK_PATH "/var/run/pf-aliasd.sock"
#define DEFAULT_PF_DEV    "/dev/pf"
#define PROTOCOL_MAGIC_0  'P'
#define PROTOCOL_MAGIC_1  'F'
#define PROTOCOL_VERSION  1

#define CMD_ADD   0x01
#define CMD_DEL   0x02
#define CMD_FLUSH 0x03
#define CMD_ACK   0x80

#define STATUS_OK  0x00
#define STATUS_ERR 0x01

#define MAX_IPS 256
#define MAX_TABLE_LEN 32

static volatile sig_atomic_t g_running = 1;

static void sig_handler(int sig) {
    (void)sig;
    g_running = 0;
}

// In-memory record tracking for TTL expiration
typedef struct ip_entry {
    char table[MAX_TABLE_LEN];
    uint8_t family; // 4 or 6
    union {
        struct in_addr ip4;
        struct in6_addr ip6;
    } addr;
    time_t expire_at;
    struct ip_entry *next;
} ip_entry_t;

static ip_entry_t *g_head = NULL;

static void add_or_renew_ip(const char *table, uint8_t family, const void *addr, uint32_t ttl) {
    time_t exp = time(NULL) + ttl;
    ip_entry_t *curr = g_head;
    while (curr) {
        if (strcmp(curr->table, table) == 0 && curr->family == family) {
            int match = 0;
            if (family == 4 && memcmp(&curr->addr.ip4, addr, 4) == 0) match = 1;
            if (family == 6 && memcmp(&curr->addr.ip6, addr, 16) == 0) match = 1;
            if (match) {
                if (exp > curr->expire_at) curr->expire_at = exp;
                return;
            }
        }
        curr = curr->next;
    }

    // New entry
    ip_entry_t *new_node = malloc(sizeof(ip_entry_t));
    if (!new_node) return;
    strncpy(new_node->table, table, sizeof(new_node->table) - 1);
    new_node->family = family;
    if (family == 4) memcpy(&new_node->addr.ip4, addr, 4);
    else memcpy(&new_node->addr.ip6, addr, 16);
    new_node->expire_at = exp;
    new_node->next = g_head;
    g_head = new_node;
}

static void sweep_expired(int pf_fd) {
    (void)pf_fd;
    time_t now = time(NULL);
    ip_entry_t **tracer = &g_head;
    while (*tracer) {
        ip_entry_t *entry = *tracer;
        if (entry->expire_at <= now) {
            // Delete from PF table
#ifdef __FreeBSD__
            struct pfr_table tbl;
            memset(&tbl, 0, sizeof(tbl));
            strncpy(tbl.pfrt_name, entry->table, sizeof(tbl.pfrt_name) - 1);

            struct pfr_addr addr;
            memset(&addr, 0, sizeof(addr));
            if (entry->family == 4) {
                addr.pfra_af = AF_INET;
                addr.pfra_net = 32;
                memcpy(&addr.pfra_ip4addr, &entry->addr.ip4, 4);
            } else {
                addr.pfra_af = AF_INET6;
                addr.pfra_net = 128;
                memcpy(&addr.pfra_ip6addr, &entry->addr.ip6, 16);
            }
            int ndel = 0;
            pfctl_table_del_addrs(pf_fd, &tbl, &addr, 1, &ndel, 0);
#endif
            *tracer = entry->next;
            free(entry);
        } else {
            tracer = &entry->next;
        }
    }
}

int main(int argc, char **argv) {
    const char *sock_path = DEFAULT_SOCK_PATH;
    const char *pf_dev_path = DEFAULT_PF_DEV;

    if (argc > 1) sock_path = argv[1];

    signal(SIGINT, sig_handler);
    signal(SIGTERM, sig_handler);

    int pf_fd = -1;
#ifdef __FreeBSD__
    pf_fd = open(pf_dev_path, O_RDWR);
    if (pf_fd < 0) {
        perror("Failed to open /dev/pf");
        return 1;
    }
    printf("[pf-aliasd-c] Opened /dev/pf (fd=%d)\n", pf_fd);
#else
    printf("[pf-aliasd-c] Non-FreeBSD build: running with mock PF engine\n");
#endif

    unlink(sock_path);
    int sfd = socket(AF_UNIX, SOCK_SEQPACKET, 0);
    if (sfd < 0) {
        perror("Failed to create SOCK_SEQPACKET socket");
        return 1;
    }

    struct sockaddr_un sun;
    memset(&sun, 0, sizeof(sun));
    sun.sun_family = AF_UNIX;
    strncpy(sun.sun_path, sock_path, sizeof(sun.sun_path) - 1);

    if (bind(sfd, (struct sockaddr *)&sun, sizeof(sun)) < 0) {
        perror("Failed to bind socket");
        return 1;
    }
    chmod(sock_path, 0660);

    if (listen(sfd, 128) < 0) {
        perror("Listen error");
        return 1;
    }

    // Set non-blocking for accept with timeout
    int flags = fcntl(sfd, F_GETFL, 0);
    fcntl(sfd, F_SETFL, flags | O_NONBLOCK);

    printf("[pf-aliasd-c] Listening on %s (SEQPACKET)...\n", sock_path);

    time_t last_gc = time(NULL);

    while (g_running) {
        int cfd = accept(sfd, NULL, NULL);
        if (cfd >= 0) {
            uint8_t buf[2048];
            ssize_t n = read(cfd, buf, sizeof(buf));
            if (n >= 10 && buf[0] == PROTOCOL_MAGIC_0 && buf[1] == PROTOCOL_MAGIC_1) {
                uint8_t cmd = buf[3];
                uint32_t seq = (buf[4] << 24) | (buf[5] << 16) | (buf[6] << 8) | buf[7];

                if (cmd == CMD_ADD && n > 17) {
                    uint8_t tlen = buf[10];
                    char table[MAX_TABLE_LEN] = {0};
                    if (tlen < MAX_TABLE_LEN && 11 + tlen + 6 <= (size_t)n) {
                        memcpy(table, &buf[11], tlen);
                        size_t off = 11 + tlen;
                        uint32_t ttl = (buf[off] << 24) | (buf[off+1] << 16) | (buf[off+2] << 8) | buf[off+3];
                        off += 4;
                        uint16_t ip_count = (buf[off] << 8) | buf[off+1];
                        off += 2;

#ifdef __FreeBSD__
                        struct pfr_table tbl;
                        memset(&tbl, 0, sizeof(tbl));
                        strncpy(tbl.pfrt_name, table, sizeof(tbl.pfrt_name) - 1);
                        struct pfr_addr addrs[MAX_IPS];
                        int pf_cnt = 0;
#endif

                        for (uint16_t i = 0; i < ip_count && off < (size_t)n; i++) {
                            uint8_t fam = buf[off++];
                            if (fam == 4 && off + 4 <= (size_t)n) {
                                add_or_renew_ip(table, 4, &buf[off], ttl);
#ifdef __FreeBSD__
                                if (pf_cnt < MAX_IPS) {
                                    memset(&addrs[pf_cnt], 0, sizeof(struct pfr_addr));
                                    addrs[pf_cnt].pfra_af = AF_INET;
                                    addrs[pf_cnt].pfra_net = 32;
                                    memcpy(&addrs[pf_cnt].pfra_ip4addr, &buf[off], 4);
                                    pf_cnt++;
                                }
#endif
                                off += 4;
                            } else if (fam == 6 && off + 16 <= (size_t)n) {
                                add_or_renew_ip(table, 6, &buf[off], ttl);
#ifdef __FreeBSD__
                                if (pf_cnt < MAX_IPS) {
                                    memset(&addrs[pf_cnt], 0, sizeof(struct pfr_addr));
                                    addrs[pf_cnt].pfra_af = AF_INET6;
                                    addrs[pf_cnt].pfra_net = 128;
                                    memcpy(&addrs[pf_cnt].pfra_ip6addr, &buf[off], 16);
                                    pf_cnt++;
                                }
#endif
                                off += 16;
                            }
                        }

#ifdef __FreeBSD__
                        if (pf_cnt > 0) {
                            int nadd = 0;
                            pfctl_table_add_addrs(pf_fd, &tbl, addrs, pf_cnt, &nadd, 0);
                        }
#endif
                    }
                }

                // Send ACK
                uint8_t ack[12];
                ack[0] = PROTOCOL_MAGIC_0;
                ack[1] = PROTOCOL_MAGIC_1;
                ack[2] = PROTOCOL_VERSION;
                ack[3] = CMD_ACK;
                ack[4] = (seq >> 24) & 0xFF;
                ack[5] = (seq >> 16) & 0xFF;
                ack[6] = (seq >> 8) & 0xFF;
                ack[7] = seq & 0xFF;
                ack[8] = STATUS_OK;
                ack[9] = 0; // rsvd
                ack[10] = 0; // errlen hi
                ack[11] = 0; // errlen lo
                write(cfd, ack, sizeof(ack));
            }
            close(cfd);
        } else {
            usleep(50000); // 50ms sleep
        }

        // Periodic TTL Expiration GC
        time_t now = time(NULL);
        if (now - last_gc >= 2) {
            sweep_expired(pf_fd);
            last_gc = now;
        }
    }

    close(sfd);
    unlink(sock_path);
    if (pf_fd >= 0) close(pf_fd);
    printf("[pf-aliasd-c] Cleaned up and exited.\n");
    return 0;
}
