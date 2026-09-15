/* Short-lived coordinator guard. No daemon, stale-lock deletion or PID signals.
 * Advisory POSIX locks survive exec in this process and die with it.
 * The lock file must remain at the same pathname/inode for the installation.
 */
#define _POSIX_C_SOURCE 200809L
#include <errno.h>
#include <fcntl.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <time.h>
#include <unistd.h>
#include <limits.h>

static int sync_path(const char *path, int directory) {
    int fd=open(path,O_RDONLY|O_NOFOLLOW|(directory?O_DIRECTORY:0));
    struct stat st;
    if (fd<0) return -1;
    if (fstat(fd,&st) || st.st_uid!=geteuid() ||
        (directory?!S_ISDIR(st.st_mode):(!S_ISREG(st.st_mode)||st.st_nlink!=1))) {
        close(fd); return -1;
    }
    int rc=fsync(fd); close(fd); return rc;
}

/* Publish a fully persisted fence with one non-replacing symlink syscall.
 * Caller holds the coordinator lock. Never follows an existing destination.
 * The old updater rejects any present symlink, preserving its admission fence.
 */
static int publish_fence(const char *target,const char *linkpath) {
    const char *names[]={"owner.json","pid","scope","action","bundle","startedAt",NULL};
    char path[PATH_MAX],parent[PATH_MAX];
    if (target[0]!='/' || linkpath[0]!='/' || strlen(target)>=sizeof parent ||
        strlen(linkpath)>=sizeof parent) return 64;
    for (int i=0;names[i];i++) {
        if (snprintf(path,sizeof path,"%s/%s",target,names[i]) >= (int)sizeof path || sync_path(path,0)) return 74;
    }
    if (sync_path(target,1)) return 74;
    strcpy(parent,target); char *slash=strrchr(parent,'/');
    if (!slash || slash==parent) return 64;
    *slash=0;
    for (int i=0;i<2;i++) {
        if (snprintf(path,sizeof path,"%s/%s",parent,i?"state.json":"owner.json") >= (int)sizeof path || sync_path(path,0)) return 74;
    }
    /* Persist each directory entry back to the filesystem root. */
    while (parent[0]) {
        if (sync_path(parent,1)) return 74;
        slash=strrchr(parent,'/'); if (!slash || slash==parent) break; *slash=0;
    }
    if (symlink(target,linkpath)) return errno==EEXIST?75:74;
    strcpy(parent,linkpath);slash=strrchr(parent,'/');
    if (slash==parent) slash[1]=0; else *slash=0;
    return sync_path(parent,1)?74:0;
}

int main(int argc, char **argv) {
    int fd, flags, attempts = 0;
    struct stat before, after;
    struct flock lock;
    struct timespec pause = {0, 10000000};
    if (argc == 2 && strcmp(argv[1], "--version") == 0) {
        puts("broray-ops-guard/2 fcntl-exec atomic-fence");
        return 0;
    }
    if (argc==4 && strcmp(argv[1],"--publish-fence")==0) return publish_fence(argv[2],argv[3]);
    if (argc < 3 || argv[1][0] != '/' || argv[2][0] != '/') return 64;
    umask(077);
    fd = open(argv[1], O_RDWR | O_CREAT | O_NOFOLLOW, 0600);
    if (fd < 0) return 74;
    if (fstat(fd, &before) || !S_ISREG(before.st_mode) || before.st_nlink != 1 ||
        before.st_uid != geteuid() || (before.st_mode & 0077)) return 74;
    memset(&lock, 0, sizeof lock);
    lock.l_type = F_WRLCK;
    lock.l_whence = SEEK_SET;
    while (fcntl(fd, F_SETLK, &lock) < 0) {
        if (errno != EACCES && errno != EAGAIN && errno != EINTR) return 74;
        if (++attempts >= 200) return 75;
        nanosleep(&pause, NULL);
    }
    /* Detect a changed pathname; never create two independently locked inodes. */
    if (lstat(argv[1], &after) || !S_ISREG(after.st_mode) ||
        before.st_dev != after.st_dev || before.st_ino != after.st_ino || after.st_nlink != 1) return 74;
    flags = fcntl(fd, F_GETFD);
    if (flags < 0 || fcntl(fd, F_SETFD, flags & ~FD_CLOEXEC) < 0) return 74;
    if (setenv("BRORAY_OPS_GUARD_HELD", "1", 1)) return 74;
    execv(argv[2], argv + 2);
    perror("broray-ops-guard exec");
    return 74;
}
