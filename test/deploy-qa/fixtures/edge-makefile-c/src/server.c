#include <arpa/inet.h>
#include <netinet/in.h>
#include <signal.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/socket.h>
#include <unistd.h>

static const char BODY[] =
    "<!doctype html><title>hello</title><h1>PANDO-QA edge-makefile-c OK</h1>\n";

int main(void) {
    const char *port_env = getenv("PORT");
    int port = port_env ? atoi(port_env) : 8080;
    signal(SIGPIPE, SIG_IGN);

    int fd = socket(AF_INET, SOCK_STREAM, 0);
    if (fd < 0) { perror("socket"); return 1; }
    int one = 1;
    setsockopt(fd, SOL_SOCKET, SO_REUSEADDR, &one, sizeof one);

    struct sockaddr_in addr;
    memset(&addr, 0, sizeof addr);
    addr.sin_family = AF_INET;
    addr.sin_addr.s_addr = htonl(INADDR_ANY);
    addr.sin_port = htons((unsigned short)port);
    if (bind(fd, (struct sockaddr *)&addr, sizeof addr) < 0) { perror("bind"); return 1; }
    if (listen(fd, 64) < 0) { perror("listen"); return 1; }
    printf("listening on %d\n", port);
    fflush(stdout);

    char header[256];
    int hlen = snprintf(header, sizeof header,
        "HTTP/1.1 200 OK\r\nContent-Type: text/html; charset=utf-8\r\n"
        "Content-Length: %zu\r\nConnection: close\r\n\r\n", sizeof BODY - 1);

    for (;;) {
        int c = accept(fd, NULL, NULL);
        if (c < 0) continue;
        char buf[4096];
        (void)read(c, buf, sizeof buf);  /* single-read request; enough for GET */
        (void)write(c, header, (size_t)hlen);
        (void)write(c, BODY, sizeof BODY - 1);
        close(c);
    }
}
