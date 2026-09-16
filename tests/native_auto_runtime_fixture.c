/* Isolated identity fixture, never a VPN implementation or release artifact. */
#include <stdio.h>
#include <string.h>
#include <unistd.h>
int main(int argc, char **argv) {
    if (argc == 2 && !strcmp(argv[1], "version")) {
        puts("Xray 26.9.9 (auto-switch fixture) linux/amd64");
        return 0;
    }
    for (int i = 1; i < argc; i++)
        if (!strcmp(argv[i], "-test")) return 0;
    if (argc != 4 || strcmp(argv[1], "run") || strcmp(argv[2], "-c")) return 64;
    for (int i = 0; i < 300; i++) sleep(1);
    return 0;
}
