/* Short-lived coordinator guard. No daemon, stale-lock deletion or PID signals.
 * The descriptor owns the lock across fork/exec, including native publishers
 * orphaned by a coordinator crash. The last inherited close releases it.
 * The lock file must remain at the same pathname/inode for the installation.
 */
#define _POSIX_C_SOURCE 200809L
#include <errno.h>
#include <fcntl.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <sys/file.h>
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

/* Durable state transitions: success permits the caller to start work/commit.
 * A temporary file is on the same filesystem, private, and never a hardlink.
 * Both names are under the coordinator's private directory and serialized by
 * its persistent lock. A failed directory fsync is an error, never admission.
 */
static int replace_file(const char *temporary,const char *target) {
    struct stat st;
    char from_parent[PATH_MAX],to_parent[PATH_MAX];
    if (temporary[0]!='/' || target[0]!='/' || strlen(temporary)>=PATH_MAX ||
        strlen(target)>=PATH_MAX || !strcmp(temporary,target)) return 64;
    strcpy(from_parent,temporary);strcpy(to_parent,target);
    char *a=strrchr(from_parent,'/'),*b=strrchr(to_parent,'/');
    if (!a || a==from_parent || !b || b==to_parent) return 64;
    *a=0;*b=0;
    if (strcmp(from_parent,to_parent)) return 64;
    if (lstat(temporary,&st) || !S_ISREG(st.st_mode) || st.st_uid!=geteuid() ||
        st.st_nlink!=1 || (st.st_mode&0077) || sync_path(temporary,0)) return 74;
    if (lstat(target,&st)==0) {
        if (!S_ISREG(st.st_mode) || st.st_uid!=geteuid() || st.st_nlink!=1 || (st.st_mode&0077)) return 74;
    } else if (errno!=ENOENT) return 74;
    if (rename(temporary,target)) return 74;
    return sync_path(to_parent,1)?74:0;
}

/* Confirm an observed old/new publication after an interrupted rename/fsync.
 * Absence is also persisted through the containing directory. The coordinator
 * authorizes the fixed resource and checks its hash under the same flock.
 */
static int sync_state(const char *target) {
    struct stat st;
    char parent[PATH_MAX];
    if (target[0]!='/' || strlen(target)>=sizeof parent) return 64;
    strcpy(parent,target);char *slash=strrchr(parent,'/');
    if (!slash || slash==parent || !slash[1]) return 64;
    *slash=0;
    if (lstat(target,&st)==0) {
        if (!S_ISREG(st.st_mode) || st.st_uid!=geteuid() || st.st_nlink!=1 ||
            (st.st_mode&0077) || sync_path(target,0)) return 74;
    } else if (errno!=ENOENT) return 74;
    return sync_path(parent,1)?74:0;
}

/* A single bounded JSONL record. Caller holds the inherited coordinator flock.
 * Its sequence reservation has already been persisted before entering here.
 * Partial writes and failed fsync therefore leave detectable pending evidence.
 */
static int append_file(const char *source,const char *target) {
    struct stat in,out,named;
    char data[2049],parent[PATH_MAX];
    int from=-1,to=-1,rc=74;
    if (source[0]!='/' || target[0]!='/' || strlen(target)>=PATH_MAX || !strcmp(source,target)) return 64;
    from=open(source,O_RDONLY|O_NOFOLLOW);
    if (from<0 || fstat(from,&in) || !S_ISREG(in.st_mode) || in.st_nlink!=1 ||
        in.st_uid!=geteuid() || (in.st_mode&0077) || in.st_size<2 || in.st_size>2048) goto done;
    size_t size=(size_t)in.st_size,have=0;
    while (have<size) {
        ssize_t n=read(from,data+have,size-have);
        if (n<0 && errno==EINTR) continue;
        if (n<=0) goto done;
        have+=(size_t)n;
    }
    if (data[size-1]!='\n' || memchr(data,'\n',size-1) || memchr(data,0,size)) goto done;
    to=open(target,O_WRONLY|O_CREAT|O_APPEND|O_NOFOLLOW,0600);
    if (to<0 || fstat(to,&out) || !S_ISREG(out.st_mode) || out.st_nlink!=1 ||
        out.st_uid!=geteuid() || (out.st_mode&0077) || out.st_size<0 || out.st_size>262144-(off_t)size ||
        lstat(target,&named) || named.st_dev!=out.st_dev || named.st_ino!=out.st_ino) goto done;
    have=0;
    while (have<size) {
        ssize_t n=write(to,data+have,size-have);
        if (n<0 && errno==EINTR) continue;
        if (n<=0) goto done;
        have+=(size_t)n;
    }
    if (fsync(to)) goto done;
    strcpy(parent,target);char *slash=strrchr(parent,'/');
    if (!slash || slash==parent) goto done;
    *slash=0;
    if (sync_path(parent,1)) goto done;
    rc=0;
done:
    if (from>=0) close(from);
    if (to>=0) close(to);
    return rc;
}

int main(int argc, char **argv) {
    int fd, flags, attempts = 0;
    struct stat before, after;
    struct timespec pause = {0, 10000000};
    if (argc == 2 && strcmp(argv[1], "--version") == 0) {
        puts("broray-ops-guard/6 flock-fork-exec atomic-fence durable-state durable-append sync-state");
        return 0;
    }
    if (argc==4 && strcmp(argv[1],"--publish-fence")==0) return publish_fence(argv[2],argv[3]);
    if (argc==4 && strcmp(argv[1],"--replace-file")==0) return replace_file(argv[2],argv[3]);
    if (argc==3 && strcmp(argv[1],"--sync-state")==0) return sync_state(argv[2]);
    if (argc==4 && strcmp(argv[1],"--append-file")==0) return append_file(argv[2],argv[3]);
    if (argc < 3 || argv[1][0] != '/' || argv[2][0] != '/') return 64;
    umask(077);
    fd = open(argv[1], O_RDWR | O_CREAT | O_NOFOLLOW, 0600);
    if (fd < 0) return 74;
    if (fstat(fd, &before) || !S_ISREG(before.st_mode) || before.st_nlink != 1 ||
        before.st_uid != geteuid() || (before.st_mode & 0077)) return 74;
    /* Linux flock uses the open file description, unlike process-owned POSIX
     * record locks. A forked --replace-file/--publish-fence must finish before
     * a successor can enter, even when its parent dies first. No flock utility
     * or newer OFD-lock kernel ABI is needed. Do not mix guard generations on
     * an active installation: old fcntl locks and flock do not interoperate.
     */
    while (flock(fd, LOCK_EX | LOCK_NB) < 0) {
        if (errno != EWOULDBLOCK && errno != EINTR) return 74;
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
