//
// Minimal ZMQ PUB sender for FFmpeg's zmq/azmq filters, which receive runtime filter commands
// through a ZMQ SUB socket. FFmpeg ships a `zmqsend` tool, but the runtime image has no libzmq
// headers/build tools, so we build this tiny equivalent at image-build time instead.
//
// Usage: zmqsend <endpoint> <command...>
//   zmqsend tcp://127.0.0.1:5555 "Parsed_drawtext_0 text HELLO"
//
// The endpoint must match the filter's bind address (default tcp://*:5555, i.e. the sender
// connects to tcp://127.0.0.1:5555). A PUB socket drops messages until a SUB has connected and
// subscribed, so we briefly wait after connect before sending.
//
#include <zmq.h>
#include <stdio.h>
#include <string.h>
#include <unistd.h>

int main(int argc, char **argv)
{
    if (argc < 3) {
        fprintf(stderr, "usage: zmqsend <endpoint> <command...>\n");
        return 1;
    }

    // Join the remaining arguments with single spaces into one command string.
    char buf[8192];
    size_t n = 0;
    for (int i = 2; i < argc; i++) {
        if (i > 2 && n < sizeof(buf) - 1) {
            buf[n++] = ' ';
        }
        size_t l = strlen(argv[i]);
        if (n + l >= sizeof(buf)) {
            l = sizeof(buf) - 1 - n;
        }
        memcpy(buf + n, argv[i], l);
        n += l;
        if (n >= sizeof(buf) - 1) {
            break;
        }
    }
    buf[n] = 0;

    void *ctx = zmq_ctx_new();
    void *sock = zmq_socket(ctx, ZMQ_PUB);

    int linger = 200;
    zmq_setsockopt(sock, ZMQ_LINGER, &linger, sizeof(linger));

    if (zmq_connect(sock, argv[1]) != 0) {
        perror("zmq_connect");
        zmq_close(sock);
        zmq_ctx_term(ctx);
        return 1;
    }

    // Give the PUB/SUB handshake and the SUB's subscription time to complete.
    usleep(200 * 1000);

    if (zmq_send(sock, buf, n, 0) < 0) {
        perror("zmq_send");
        zmq_close(sock);
        zmq_ctx_term(ctx);
        return 1;
    }

    zmq_close(sock);
    zmq_ctx_term(ctx);
    return 0;
}
