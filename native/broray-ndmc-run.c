#define _GNU_SOURCE
#include <sys/types.h>
#include <sys/stat.h>
#include <sys/file.h>
#include <sys/wait.h>
#include <sys/prctl.h>
#include <fcntl.h>
#include <unistd.h>
#include <signal.h>
#include <errno.h>
#include <time.h>
#include <dirent.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

/* One persistent lane, held until our child tree is gone. No stored PID,
 * process-name matching, stale-directory deletion or nested ptrace. */
static volatile sig_atomic_t interrupted;
static void stopped(int sig) { interrupted=sig; }
static long long now_ms(void) {
    struct timespec t;
    if(clock_gettime(CLOCK_MONOTONIC,&t)) _exit(74);
    return (long long)t.tv_sec*1000+t.tv_nsec/1000000;
}
static void tick(void) { struct timespec t={0,20000000}; nanosleep(&t,0); }
static int absent(const char *path) {
    struct stat s;
    return lstat(path,&s)<0 && errno==ENOENT;
}
static int seconds(const char *s) {
    if(!*s || strspn(s,"0123456789")!=strlen(s)) return -1;
    long n=strtol(s,0,10);
    return n>0 && n<=3600 ? (int)n : -1;
}
/* Linux /proc PPid is used only to identify our own unreaped children.
 * There is one wait owner and SIGCHLD is never ignored: a child's PID cannot
 * be recycled between this check and kill. Descendants become direct children
 * as their parents die because this process is a subreaper. */
static int kill_children(void) {
    DIR *d=opendir("/proc"); if(!d) return -1;
    struct dirent *entry;
    while((entry=readdir(d))) {
        if(!*entry->d_name || strspn(entry->d_name,"0123456789")!=strlen(entry->d_name)) continue;
        char path[320],line[1024],*tail; int parent; char state;
        snprintf(path,sizeof path,"/proc/%s/stat",entry->d_name);
        FILE *f=fopen(path,"r"); if(!f) continue;
        char *ok=fgets(line,sizeof line,f); fclose(f);
        if(!ok || !(tail=strrchr(line,')')) || sscanf(tail+1," %c %d",&state,&parent)!=2) continue;
        if(parent==getpid() && kill((pid_t)strtol(entry->d_name,0,10),SIGKILL)<0 && errno!=ESRCH) {
            closedir(d); return -1;
        }
    }
    closedir(d); return 0;
}
int main(int argc,char **argv) {
    if(argc==2 && !strcmp(argv[1],"--version")) { puts("broray-ndmc-run/1"); return 0; }
    /* lane, old directory, lane wait seconds, command seconds, executable,
     * exact ndmc command. Output redirection belongs to the caller. */
    if(argc!=7) return 64;
    int wait_s=seconds(argv[3]),limit=seconds(argv[4]);
    if(wait_s<0 || limit<0 || argv[1][0]!='/' || argv[2][0]!='/') return 64;
    umask(077);
    struct sigaction sa={0}; sa.sa_handler=stopped; sigemptyset(&sa.sa_mask);
    if(sigaction(SIGTERM,&sa,0) || sigaction(SIGINT,&sa,0) || sigaction(SIGHUP,&sa,0)) return 74;
    sa.sa_handler=SIG_DFL; if(sigaction(SIGCHLD,&sa,0)) return 74;
    if(prctl(PR_SET_CHILD_SUBREAPER,1,0,0,0)) return 74;
    if(!absent(argv[2])) return 125;
    int fd=open(argv[1],O_RDWR|O_CREAT|O_NOFOLLOW|O_NONBLOCK,0600);
    if(fd<0) return 74;
    struct stat st;
    if(fstat(fd,&st) || !S_ISREG(st.st_mode) || st.st_uid!=geteuid() || st.st_nlink!=1 || st.st_size!=0) return 74;
    long long until=now_ms()+wait_s*1000LL;
    while(flock(fd,LOCK_EX|LOCK_NB)) {
        if(errno!=EWOULDBLOCK && errno!=EAGAIN && errno!=EINTR) return 74;
        if(interrupted) return 128+interrupted;
        if(now_ms()>=until) return 125;
        tick();
    }
    if(lstat(argv[1],&st) || !S_ISREG(st.st_mode)) return 74;
    struct stat held; if(fstat(fd,&held) || held.st_dev!=st.st_dev || held.st_ino!=st.st_ino || !absent(argv[2])) return 125;
    pid_t owner=getpid(),child=fork();
    if(child<0) return 74;
    if(!child) {
        if(prctl(PR_SET_PDEATHSIG,SIGKILL) || getppid()!=owner || setpgid(0,0)) _exit(74);
        signal(SIGTERM,SIG_DFL); signal(SIGINT,SIG_DFL); signal(SIGHUP,SIG_DFL);
        execl(argv[5],argv[5],"-c",argv[6],(char*)0); _exit(127);
    }
    /* Establish the group from both sides before using its ID. The leader
     * remains unreaped through the final group signal, preventing ID reuse. */
    if(setpgid(child,child)<0 && errno!=EACCES && errno!=ESRCH) return 74;
    int result=74,reason=0; long long start=now_ms(),term_at=0;
    for(;;) {
        siginfo_t info={0};
        if(waitid(P_PID,(id_t)child,&info,WEXITED|WNOHANG|WNOWAIT)<0) { if(errno==EINTR) continue; return 74; }
        if(info.si_pid) {
            result=reason ? reason : (info.si_code==CLD_EXITED ? info.si_status : 128+info.si_status);
            /* Zombies still reserve the group's ID. */
            if(kill(-child,SIGKILL)<0 && errno!=ESRCH) return 74;
            break;
        }
        if(!reason && (interrupted || now_ms()-start>=limit*1000LL)) {
            reason=interrupted ? 128+interrupted : 124; term_at=now_ms();
            if(kill(-child,SIGTERM)<0 && errno!=ESRCH) return 74;
        }
        if(reason && now_ms()-term_at>=1000) {
            if(kill(-child,SIGKILL)<0 && errno!=ESRCH) return 74;
        }
        tick();
    }
    /* Drain every adopted generation before releasing the lane. A kernel
     * uninterruptible child keeps this process/lane alive (fail closed). */
    for(;;) {
        if(kill_children()) return 74;
        int status; pid_t p=waitpid(-1,&status,WNOHANG);
        if(p<0 && errno==ECHILD) break;
        if(p<0 && errno!=EINTR) return 74;
        if(!p) tick();
    }
    close(fd); return result;
}
