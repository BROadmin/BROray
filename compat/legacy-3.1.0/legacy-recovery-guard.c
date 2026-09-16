/* Compatibility prototype, NOT a shipped runtime binary.
 * Freeze verified legacy admission sources, require idle legacy daemons and
 * exclude unknown Entware writers. Only then may a five-file fence be retired.
 * Never infer ownership from a PID file, age, or an application-name substring.
 * The signed-bundle check phase must be READ-ONLY outside its private session.
 * Finalization may update only recovery bookkeeping while the fence remains.
 */
#define _GNU_SOURCE
#include <dirent.h>
#include <errno.h>
#include <fcntl.h>
#include <limits.h>
#include <signal.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/file.h>
#include <sys/prctl.h>
#include <sys/ptrace.h>
#include <sys/stat.h>
#include <sys/syscall.h>
#include <sys/wait.h>
#include <time.h>
#include <unistd.h>

#define MAX_TASKS 24
#define CMD_SIZE 8192
#define SESSION_ROOT "/opt/var/lib/broray/legacy-recovery/"
#define LOCK_PARENT "/opt/var/lock/broray"
struct task {
    pid_t pid;
    unsigned long long ticks;
    char boot[128], exe[PATH_MAX], cmd[CMD_SIZE], role[32];
    int length, pinned, stopped, seconds;
};
struct fence {
    struct stat directory;
    char data[5][4096];
    int size[5];
    pid_t owner;
};
struct projection {struct stat file;char bytes[128];int length;pid_t pid;};
static const char *projection_names[]={"home-snapshotd.pid","subscription-scheduler.pid",
    "server-auto-switch.pid","connection-monitor.pid","subscription-scheduler.starttime"};
static const char *projection_roles[]={"stop-home","stop-subscriptions","stop-auto","stop-monitor"};
static struct projection projections[5];
static struct task tasks[MAX_TASKS], ancestors[64];
static int task_count, ancestor_count;
static const char *fence_names[]={"pid","scope","action","bundle","startedAt"};
static uint64_t millis(void) {
    struct timespec t;
    if(clock_gettime(CLOCK_MONOTONIC,&t))_exit(74);
    return (uint64_t)t.tv_sec*1000+t.tv_nsec/1000000;
}
static void nap(void) { struct timespec t={0,10000000}; nanosleep(&t,NULL); }
static int numeric(const char *s) {
    if(!*s)return 0;
    for(;*s;s++)if(*s<'0'||*s>'9')return 0;
    return 1;
}
static const char *base(const char *s) { const char *p=strrchr(s,'/'); return p?p+1:s; }
static int read_bytes(const char *path,char *out,size_t cap,int ordinary) {
    int fd=open(path,O_RDONLY|O_CLOEXEC|O_NONBLOCK|(ordinary?O_NOFOLLOW:0));
    if(fd<0)return -1;
    struct stat st;
    if(fstat(fd,&st)||!S_ISREG(st.st_mode)||(ordinary&&(st.st_uid!=geteuid()||st.st_nlink!=1))) {close(fd);return -1;}
    size_t used=0;
    for(;;) {
        ssize_t n=read(fd,out+used,cap-used);
        if(n<0&&errno==EINTR)continue;
        if(n<0){close(fd);return -1;}
        if(!n)break;
        used+=(size_t)n;
        if(used==cap){close(fd);return -1;}
    }
    close(fd);return (int)used;
}
static int fields(pid_t pid,unsigned long long *ticks,pid_t *ppid,char *state) {
    char path[64],buf[4096],*end;
    snprintf(path,sizeof path,"/proc/%d/stat",pid);
    int n=read_bytes(path,buf,sizeof buf-1,0);
    if(n<=0)return -1;
    buf[n]=0;
    char *p=strrchr(buf,')');if(!p||p[1]!=' ')return -1;p+=2;
    *state=*p;long parent=strtol(p+2,&end,10);
    if(end==p+2||*end!=' '||parent<0||parent>INT_MAX)return -1;
    *ppid=(pid_t)parent;
    for(int i=1;i<20;i++){p=strchr(p,' ');if(!p)return -1;p++;}
    *ticks=strtoull(p,&end,10);
    return end==p||*end!=' '||!*ticks?-1:0;
}
static int gone_or_kernel(pid_t pid) {
    char path[64],buf[4096];struct stat st;
    snprintf(path,sizeof path,"/proc/%d/stat",pid);
    int n=read_bytes(path,buf,sizeof buf-1,0);
    if(n<=0) {
        snprintf(path,sizeof path,"/proc/%d",pid);
        return lstat(path,&st)<0&&errno==ENOENT;
    }
    buf[n]=0;char *p=strrchr(buf,')');if(!p||p[1]!=' ')return 0;p+=2;
    if(*p=='Z'||*p=='X')return 1;
    for(int i=0;i<6;i++){p=strchr(p,' ');if(!p)return 0;p++;}
    char *end;unsigned long flags=strtoul(p,&end,10);
    /* Linux PF_KTHREAD. An unreadable executable alone is not evidence that
     * a task is harmless: unreadable userspace processes retain the fence. */
    return end!=p&&*end==' '&&(flags&0x00200000UL)!=0;
}
static int single_thread(pid_t pid) {
    char path[64];snprintf(path,sizeof path,"/proc/%d/task",pid);
    DIR *d=opendir(path);if(!d)return 0;
    struct dirent *e;int count=0;
    while((e=readdir(d)))if(numeric(e->d_name))count++;
    closedir(d);return count==1;
}
static int capture(pid_t pid,struct task *t) {
    char path[64],state;pid_t parent;unsigned long long again;
    memset(t,0,sizeof *t);t->pid=pid;
    if(fields(pid,&t->ticks,&parent,&state)||state=='Z'||state=='X')return -1;
    snprintf(path,sizeof path,"/proc/%d/exe",pid);
    ssize_t n=readlink(path,t->exe,sizeof t->exe-1);if(n<=0||n>=(ssize_t)sizeof t->exe-1)return -1;t->exe[n]=0;
    snprintf(path,sizeof path,"/proc/%d/cmdline",pid);
    t->length=read_bytes(path,t->cmd,sizeof t->cmd,0);
    /* Linux 4.9 may expose a rewritten process title without its terminating
     * NUL (observed in Keenetic's own nginx). Preserve the exact bounded raw
     * bytes for identity; role_valid/args still require valid argv for targets. */
    if(t->length<=0)return -1;
    n=read_bytes("/proc/sys/kernel/random/boot_id",t->boot,sizeof t->boot-1,0);
    if(n<=0)return -1;
    while(n>0&&(t->boot[n-1]=='\n'||t->boot[n-1]=='\r'))n--;
    t->boot[n]=0;
    return fields(pid,&again,&parent,&state)||again!=t->ticks||state=='Z'||state=='X'?-1:0;
}
static int same(const struct task *expected) {
    struct task live;
    return !capture(expected->pid,&live)&&live.ticks==expected->ticks&&
        !strcmp(live.boot,expected->boot)&&!strcmp(live.exe,expected->exe)&&
        live.length==expected->length&&!memcmp(live.cmd,expected->cmd,(size_t)live.length);
}
static int args(const struct task *t,const char **items,int capacity) {
    int count=0,offset=0;
    while(offset<t->length) {
        if(count==capacity)return -1;
        items[count++]=t->cmd+offset;
        size_t n=strnlen(t->cmd+offset,(size_t)(t->length-offset));
        if(n==(size_t)(t->length-offset))return -1;
        offset+=(int)n+1;
    }
    return count;
}
static int role_valid(const struct task *t) {
    if(!strcmp(t->role,"hold-proxy")) {
        /* Keenetic 4.9 exposes the master title without a final NUL, while
         * workers may retain NUL padding. Accept only the frozen service's
         * complete title and executable, preserving exact raw identity. */
        const char *master="nginx: master process /opt/broray/run/web-new/native-auth/broray-ndm-auth-nginx -p / -c /opt/broray/run/web-new/native-auth/nginx.conf";
        const char *worker="nginx: worker process";
        size_t first=strnlen(t->cmd,(size_t)t->length);
        if(strcmp(t->exe,"/opt/broray/run/web-new/native-auth/broray-ndm-auth-nginx")||t->seconds)return 0;
        if(!((first==strlen(master)&&!memcmp(t->cmd,master,first))||
             (first==strlen(worker)&&!memcmp(t->cmd,worker,first))))return 0;
        for(size_t i=first;i<(size_t)t->length;i++)if(t->cmd[i])return 0;
        return 1;
    }
    const char *a[8];int n=args(t,a,8);
    if(n<1||t->exe[0]!='/')return 0;
    const char *script=NULL;int seconds=0;
    if(!strcmp(t->role,"stop-home")){script="broray-home-snapshotd";seconds=30;}
    if(!strcmp(t->role,"stop-subscriptions")){script="broray-subscription-scheduler";seconds=60;}
    if(!strcmp(t->role,"stop-auto")){script="broray-server-auto-switch";seconds=15;}
    if(!strcmp(t->role,"stop-monitor")){script="broray-connection-monitor";seconds=10;}
    if(script) {
        if(n!=2||strcmp(a[0],"/opt/bin/ash")||strcmp(base(t->exe),"busybox")||t->seconds!=seconds)return 0;
        char p[PATH_MAX],q[PATH_MAX];
        snprintf(p,sizeof p,"/opt/broray/bin/%s",script);
        snprintf(q,sizeof q,"/opt/broray/current/app/bin/%s",script);
        return !strcmp(a[1],p)||!strcmp(a[1],q);
    }
    if(!strcmp(t->role,"hold-updater"))return n==3&&!strcmp(a[0],"/opt/bin/ash")&&
        !strcmp(a[1],"/opt/libexec/broray-updater/broray-updater.sh")&&!strcmp(a[2],"daemon")&&
        !strcmp(base(t->exe),"busybox")&&t->seconds==2;
    if(!strcmp(t->role,"hold-web"))return n==3&&!strcmp(t->exe,"/opt/broray/runtime/broray-lighttpd")&&
        !strcmp(a[0],t->exe)&&!strcmp(a[1],"-f")&&!strcmp(a[2],"/opt/broray/config/lighttpd.conf")&&t->seconds==0;
    if(!strcmp(t->role,"hold-ssh")) {
        pid_t parent;unsigned long long ticks;char state;
        return !strcmp(t->exe,"/opt/sbin/dropbear")&&!fields(t->pid,&ticks,&parent,&state)&&parent==1&&t->seconds==0;
    }
    if(!strcmp(t->role,"preserve-xray"))return n==4&&!strcmp(t->exe,"/opt/broray/runtime/xray")&&
        !strcmp(a[0],t->exe)&&!strcmp(a[1],"run")&&!strcmp(a[2],"-c")&&
        !strcmp(a[3],"/opt/broray/config/config.json")&&t->seconds==0;
    return 0;
}
static int children(pid_t pid,pid_t *only) {
    DIR *d=opendir("/proc");if(!d)return -1;
    struct dirent *e;int count=0;
    while((e=readdir(d))) {
        if(!numeric(e->d_name))continue;
        long value=strtol(e->d_name,NULL,10);if(value<=1||value>INT_MAX)continue;
        pid_t parent;unsigned long long ticks;char state;
        if(fields((pid_t)value,&ticks,&parent,&state)) {
            if(!gone_or_kernel((pid_t)value)){closedir(d);return -1;}
        } else if(parent==pid){*only=(pid_t)value;count++;}
    }
    closedir(d);return count;
}
static int sleep_task(const struct task *t,int seconds) {
    const char *a[4];int n=args(t,a,4);pid_t unused=0;
    if(n!=2||strcmp(base(t->exe),"busybox")||strcmp(base(a[0]),"sleep")||!numeric(a[1]))return 0;
    if(seconds&&strtol(a[1],NULL,10)!=seconds)return 0;
    return single_thread(t->pid)&&children(t->pid,&unused)==0&&same(t);
}
static int idle(const struct task *t) {
    pid_t child=0;struct task sleep;
    return children(t->pid,&child)==1&&!capture(child,&sleep)&&!strcmp(sleep.exe,t->exe)&&sleep_task(&sleep,t->seconds);
}
static void detach_all(void) {
    for(int i=task_count-1;i>=0;i--)if(tasks[i].pinned) {
        ptrace(PTRACE_DETACH,tasks[i].pid,0,0);tasks[i].pinned=0;
    }
}
static int pin(struct task *t) {
    pid_t parent;unsigned long long ticks;char state;
    if(!same(t)||!single_thread(t->pid)||fields(t->pid,&ticks,&parent,&state)||state=='T'||state=='t')return -1;
    if(ptrace(PTRACE_SEIZE,t->pid,0,0)<0)return -1;
    t->pinned=1;
    if(ptrace(PTRACE_INTERRUPT,t->pid,0,0)<0)return -1;
    uint64_t until=millis()+3000;int status;
    while(millis()<until) {
        pid_t p=waitpid(t->pid,&status,__WALL|WNOHANG);
        if(p==t->pid)return WIFSTOPPED(status)&&same(t)?0:-1;
        if(p<0&&errno!=EINTR)return -1;
        nap();
    }
    return -1;
}
static int ancestors_capture(void) {
    pid_t pid=getpid();
    while(pid>1) {
        if(ancestor_count==64)return -1;
        if(capture(pid,&ancestors[ancestor_count]))return -1;
        if(ancestor_count>0) {
            const struct task *a=&ancestors[ancestor_count];
            /* A CGI broker/daemon ancestor could keep admitting writers while
             * waiting for this child. Such an invocation is not a standalone
             * Keenetic Web CLI/SSH bootstrap and cannot exempt the broker. */
            if(!strncmp(a->exe,"/opt/broray/",12)||
               memmem(a->cmd,(size_t)a->length,"/opt/broray/",12)||
               memmem(a->cmd,(size_t)a->length,"/opt/libexec/broray-updater/",sizeof("/opt/libexec/broray-updater/")-1))return -1;
            if(!strncmp(a->exe,"/opt/",5)&&strcmp(base(a->exe),"busybox")&&strcmp(a->exe,"/opt/sbin/dropbear"))return -1;
        }
        ancestor_count++;
        pid_t parent;unsigned long long ticks;char state;
        if(fields(pid,&ticks,&parent,&state)||parent==pid)return -1;
        pid=parent;
    }
    return 0;
}
static int inventory_clear(void) {
    DIR *d=opendir("/proc");if(!d)return -1;
    struct dirent *e;int rc=0;
    while((e=readdir(d))) {
        if(!numeric(e->d_name))continue;
        long value=strtol(e->d_name,NULL,10);if(value<=1||value>INT_MAX)continue;
        struct task live;
        if(capture((pid_t)value,&live)) {
            if(!gone_or_kernel((pid_t)value)){rc=-1;break;}
            continue;
        }
        /* Old helpers may use Keenetic's native utilities outside /opt.
         * In particular server-service.sh can leave an ip route writer.
         * An idle cron/inetd broker also cannot be exempted as a non-Entware
         * executable: it can admit a new writer after this scan. */
        static const char *helpers[]={"busybox","ndmc","curl","wget","jq","ip","route","tc","nft",
            "iptables","ip6tables","iptables-restore","ip6tables-restore","opkg","crond","inetd","atd",
            "ash","sh","bash","dash","python","python3","perl","node","awk","sed","find","xargs",
            "mv","cp","rm","mkdir","rmdir","ln","install","chmod","chown","tee","tar","unzip","dd",
            "truncate","touch","start-stop-daemon","modprobe","insmod","kill","killall"};
        int relevant=!strncmp(live.exe,"/opt/",5);
        for(size_t i=0;i<sizeof helpers/sizeof helpers[0];i++)if(!strcmp(base(live.exe),helpers[i]))relevant=1;
        if(!relevant)continue;
        int known=0;
        for(int i=0;i<ancestor_count;i++)if(live.pid==ancestors[i].pid&&same(&ancestors[i]))known=1;
        for(int i=0;i<task_count;i++)if(live.pid==tasks[i].pid&&same(&tasks[i]))known=1;
        if(!known&&!sleep_task(&live,0)) {
            fprintf(stderr,"LEGACY_WRITER_UNCONFIRMED pid=%d\n",live.pid);rc=-1;break;
        }
    }
    closedir(d);return rc;
}
static int fence_read(int parent,struct fence *f) {
    int fd=openat(parent,"global-operation.lock",O_RDONLY|O_DIRECTORY|O_NOFOLLOW|O_CLOEXEC);
    if(fd<0)return -1;
    if(fstat(fd,&f->directory)||f->directory.st_uid!=geteuid()){close(fd);return -1;}
    DIR *d=fdopendir(dup(fd));if(!d){close(fd);return -1;}
    struct dirent *e;int count=0,rc=0;
    while((e=readdir(d))) {
        if(!strcmp(e->d_name,".")||!strcmp(e->d_name,".."))continue;
        int index=-1;for(int i=0;i<5;i++)if(!strcmp(e->d_name,fence_names[i]))index=i;
        if(index<0){rc=-1;break;}
        int file=openat(fd,e->d_name,O_RDONLY|O_NOFOLLOW|O_NONBLOCK|O_CLOEXEC);struct stat st;
        if(file<0||fstat(file,&st)||!S_ISREG(st.st_mode)||st.st_uid!=geteuid()||st.st_nlink!=1||st.st_size<(index==3?0:1)||st.st_size>=4096) {
            if(file>=0)close(file);rc=-1;break;
        }
        ssize_t n=read(file,f->data[index],sizeof f->data[index]);close(file);
        if(n!=st.st_size||(n>0&&(f->data[index][n-1]!='\n'||memchr(f->data[index],0,(size_t)n)||
           memchr(f->data[index],'\n',(size_t)n-1)))){rc=-1;break;}
        f->size[index]=(int)n;count++;
    }
    closedir(d);close(fd);
    if(rc||count!=5)return -1;
    char pid[4096];memcpy(pid,f->data[0],(size_t)f->size[0]);pid[f->size[0]-1]=0;
    if(!numeric(pid))return -1;
    long value=strtol(pid,NULL,10);if(value<=1||value>INT_MAX)return -1;
    f->owner=(pid_t)value;return 0;
}
static int fence_same(const struct fence *a,const struct fence *b) {
    if(a->directory.st_dev!=b->directory.st_dev||a->directory.st_ino!=b->directory.st_ino)return 0;
    for(int i=0;i<5;i++)if(a->size[i]!=b->size[i]||memcmp(a->data[i],b->data[i],(size_t)a->size[i]))return 0;
    return 1;
}
static int fence_sync(int parent) {
    int directory=openat(parent,"global-operation.lock",O_RDONLY|O_DIRECTORY|O_NOFOLLOW|O_CLOEXEC);
    if(directory<0)return -1;
    for(int i=0;i<5;i++) {
        int fd=openat(directory,fence_names[i],O_RDONLY|O_NOFOLLOW|O_CLOEXEC);
        if(fd<0){close(directory);return -1;}
        int rc=fsync(fd);close(fd);
        if(rc){close(directory);return -1;}
    }
    int rc=fsync(directory);close(directory);return rc;
}
static int receipt(int directory,const char *name,const char *json) {
    int fd=openat(directory,name,O_WRONLY|O_CREAT|O_EXCL|O_NOFOLLOW|O_CLOEXEC,0600);
    if(fd<0)return -1;
    size_t size=strlen(json),done=0;
    while(done<size) {
        ssize_t n=write(fd,json+done,size-done);
        if(n<0&&errno==EINTR)continue;
        if(n<=0){close(fd);return -1;}
        done+=(size_t)n;
    }
    int rc=fsync(fd);close(fd);return rc?rc:fsync(directory);
}
static int projection_capture(int run) {
    for(int i=0;i<5;i++) {
        struct projection *p=&projections[i];
        int fd=openat(run,projection_names[i],O_RDONLY|O_NOFOLLOW|O_NONBLOCK|O_CLOEXEC);
        if(fd<0) {if(errno==ENOENT){p->length=-1;continue;}return -1;}
        if(fstat(fd,&p->file)||!S_ISREG(p->file.st_mode)||p->file.st_nlink!=1||
           p->file.st_uid!=0||p->file.st_size<2||p->file.st_size>=128){close(fd);return -1;}
        p->length=(int)read(fd,p->bytes,sizeof p->bytes);close(fd);
        if(p->length!=p->file.st_size||p->bytes[p->length-1]!='\n'||memchr(p->bytes,0,(size_t)p->length))return -1;
        char number[128];memcpy(number,p->bytes,(size_t)p->length);number[p->length-1]=0;
        if(!numeric(number))return -1;
        if(i==4) {
            if(projections[1].length<0)return -1;
            for(int j=0;j<task_count;j++)if(tasks[j].pid==projections[1].pid&&
                strtoull(number,NULL,10)!=tasks[j].ticks)return -1;
            continue;
        }
        long value=strtol(number,NULL,10);if(value<=1||value>INT_MAX)return -1;p->pid=(pid_t)value;
        char path[64];snprintf(path,sizeof path,"/proc/%d",p->pid);struct stat live;
        if(lstat(path,&live)==0) {
            int known=0;
            for(int j=0;j<task_count;j++)if(tasks[j].pid==p->pid&&!strcmp(tasks[j].role,projection_roles[i]))known=1;
            if(!known)return -1;
        } else if(errno!=ENOENT)return -1;
    }
    return 0;
}
static int projection_retire(int run,int session) {
    /* Only this still-running barrier renames these records. A child callback
     * must never delete a projection after its parent dies and admission reopens. */
    for(int i=0;i<5;i++) {
        const struct projection *p=&projections[i];struct stat named;
        if(p->length<0) {
            if(fstatat(run,projection_names[i],&named,AT_SYMLINK_NOFOLLOW)==0||errno!=ENOENT)return -1;
            continue;
        }
        if(fstatat(run,projection_names[i],&named,AT_SYMLINK_NOFOLLOW)||
           !S_ISREG(named.st_mode)||named.st_nlink!=1||named.st_dev!=p->file.st_dev||named.st_ino!=p->file.st_ino)return -1;
        int fd=openat(run,projection_names[i],O_RDONLY|O_NOFOLLOW|O_NONBLOCK|O_CLOEXEC);char bytes[128];
        if(fd<0)return -1;
        ssize_t n=read(fd,bytes,sizeof bytes);int rc=fsync(fd);close(fd);
        if(n!=p->length||memcmp(bytes,p->bytes,(size_t)p->length)||rc)return -1;
        if(i<4) {
            char path[64];snprintf(path,sizeof path,"/proc/%d",p->pid);
            if(lstat(path,&named)==0) {
                pid_t parent;unsigned long long ticks;char state;
                if(fields(p->pid,&ticks,&parent,&state)||(state!='Z'&&state!='X'))return -1;
                int own=0;
                for(int j=0;j<task_count;j++)if(tasks[j].stopped&&tasks[j].pid==p->pid&&tasks[j].ticks==ticks)own=1;
                if(!own)return -1;
            } else if(errno!=ENOENT)return -1;
        }
        char target[96];snprintf(target,sizeof target,"retired-%s",projection_names[i]);
        if(syscall(SYS_renameat2,run,projection_names[i],session,target,1))return -1;
        if(fsync(run)||fsync(session))return -1;
    }
    return 0;
}
static int terminate_pinned(struct task *t) {
    if(!t->pinned||!same(t)||!idle(t))return -1;
    /* A stopped, unreaped ptrace task pins the PID through delivery. */
    if(kill(t->pid,SIGKILL)<0)return -1;
    uint64_t until=millis()+3000;int status;
    while(millis()<until) {
        pid_t p=waitpid(t->pid,&status,__WALL|WNOHANG);
        if(p==t->pid) {
            if(WIFEXITED(status)||WIFSIGNALED(status)){t->pinned=0;t->stopped=1;return 0;}
            if(WIFSTOPPED(status))ptrace(PTRACE_CONT,t->pid,0,SIGKILL);
        } else if(p<0&&errno!=EINTR)return -1;
        nap();
    }
    return -1;
}
static int run_callback(const char *session,const char *phase) {
    char script[PATH_MAX];snprintf(script,sizeof script,"%s/preflight.sh",session);
    struct stat st;if(lstat(script,&st)||!S_ISREG(st.st_mode)||st.st_uid!=geteuid()||st.st_nlink!=1||(st.st_mode&0077))return -1;
    pid_t parent=getpid(),child=fork();if(child<0)return -1;
    if(!child) {
        if(prctl(PR_SET_PDEATHSIG,SIGKILL)||getppid()!=parent)_exit(74);
        char log[PATH_MAX],parent_text[32];
        snprintf(parent_text,sizeof parent_text,"%d",parent);
        if(setenv("BRORAY_LEGACY_BARRIER_PID",parent_text,1))_exit(74);
        for(int stream=1;stream<=2;stream++) {
            snprintf(log,sizeof log,"%s/%s.%s",session,phase,stream==1?"stdout":"stderr");
            int fd=open(log,O_WRONLY|O_CREAT|O_EXCL|O_NOFOLLOW,0600);
            if(fd<0||dup2(fd,stream)<0)_exit(74);
            if(fd!=stream)close(fd);
        }
        execl("/opt/bin/ash","/opt/bin/ash",script,session,phase,(char *)NULL);_exit(74);
    }
    uint64_t until=millis()+60000;int status;
    while(millis()<until) {
        pid_t p=waitpid(child,&status,WNOHANG);
        if(p==child) {
            pid_t leftover=0;
            /* A callback cannot hide a writer by exiting before its child.
             * PR_SET_CHILD_SUBREAPER below adopts any such descendants. */
            return WIFEXITED(status)&&WEXITSTATUS(status)==0&&children(getpid(),&leftover)==0?0:-1;
        }
        if(p<0&&errno!=EINTR)return -1;
        nap();
    }
    /* Our own fork remains unreaped, so this PID cannot refer to another task. */
    kill(child,SIGKILL);while(waitpid(child,&status,0)<0&&errno==EINTR){}
    return -1;
}
static int load_targets(const char *session) {
    char path[PATH_MAX],rows[32768];snprintf(path,sizeof path,"%s/targets.tsv",session);
    int length=read_bytes(path,rows,sizeof rows-1,1);if(length<=0)return -1;rows[length]=0;
    char *save=NULL;
    for(char *line=strtok_r(rows,"\n",&save);line;line=strtok_r(NULL,"\n",&save)) {
        if(task_count==MAX_TASKS)return -1;
        struct task *t=&tasks[task_count];char command[64],extra;long pid;
        if(sscanf(line,"%31s\t%ld\t%llu\t%127s\t%4095s\t%63s\t%d %c",t->role,&pid,&t->ticks,t->boot,t->exe,command,&t->seconds,&extra)!=7||
           pid<=1||pid>INT_MAX||!t->ticks)return -1;
        t->pid=(pid_t)pid;
        char expected[64];snprintf(expected,sizeof expected,"cmd-%d",t->pid);
        if(strcmp(command,expected))return -1;
        for(int i=0;i<task_count;i++)if(tasks[i].pid==t->pid||
            (!strcmp(t->role,"preserve-xray")&&!strcmp(tasks[i].role,"preserve-xray")))return -1;
        for(int i=0;i<ancestor_count;i++)if(ancestors[i].pid==t->pid)return -1;
        snprintf(path,sizeof path,"%s/%s",session,command);
        t->length=read_bytes(path,t->cmd,sizeof t->cmd,1);
        if(t->length<=0||!role_valid(t)||!same(t))return -1;
        task_count++;
    }
    return task_count>0?0:-1;
}
static int discover(const char *session) {
    static const char *roles[]={"stop-home","stop-subscriptions","stop-auto","stop-monitor",
        "hold-updater","hold-web","hold-proxy","hold-ssh","preserve-xray"};
    static const int seconds[]={30,60,15,10,2,0,0,0,0};
    if(ancestors_capture())return 75;
    DIR *proc=opendir("/proc");if(!proc)return 74;
    struct dirent *entry;
    while((entry=readdir(proc))) {
        if(!numeric(entry->d_name))continue;
        long value=strtol(entry->d_name,NULL,10);if(value<=1||value>INT_MAX)continue;
        int ancestor=0;
        for(int i=0;i<ancestor_count;i++)if(ancestors[i].pid==value)ancestor=1;
        if(ancestor)continue;
        struct task live;if(capture((pid_t)value,&live))continue;
        for(size_t i=0;i<sizeof roles/sizeof roles[0];i++) {
            snprintf(live.role,sizeof live.role,"%s",roles[i]);live.seconds=seconds[i];
            if(!role_valid(&live))continue;
            if(task_count==MAX_TASKS){closedir(proc);return 75;}
            tasks[task_count++]=live;break;
        }
    }
    closedir(proc);
    if(!task_count)return 75;
    char path[PATH_MAX];snprintf(path,sizeof path,"%s/targets.tsv",session);
    int fd=open(path,O_WRONLY|O_CREAT|O_EXCL|O_NOFOLLOW|O_CLOEXEC,0600);
    if(fd<0)return 74;
    FILE *rows=fdopen(fd,"w");if(!rows){close(fd);return 74;}
    for(int i=0;i<task_count;i++) {
        struct task *t=&tasks[i];
        snprintf(path,sizeof path,"%s/cmd-%d",session,t->pid);
        int cmd=open(path,O_WRONLY|O_CREAT|O_EXCL|O_NOFOLLOW|O_CLOEXEC,0600);
        if(cmd<0){fclose(rows);return 74;}
        ssize_t written=write(cmd,t->cmd,(size_t)t->length);int rc=fsync(cmd);close(cmd);
        if(written!=t->length||rc){fclose(rows);return 74;}
        if(fprintf(rows,"%s\t%d\t%llu\t%s\t%s\tcmd-%d\t%d\n",t->role,t->pid,t->ticks,t->boot,t->exe,t->pid,t->seconds)<0){fclose(rows);return 74;}
    }
    int rc=fflush(rows);if(!rc)rc=fsync(fd);if(fclose(rows))rc=-1;
    if(rc)return 74;
    puts("{\"ok\":true,\"result\":\"identity_snapshot_created\",\"prototype\":true}");return 0;
}
int main(int argc,char **argv) {
    if(argc==2&&!strcmp(argv[1],"--version")){puts("broray-legacy-recovery-prototype/2 not-shipped");return 0;}
    int discovery=argc==3&&!strcmp(argv[1],"--discover");
    if((argc!=2&&!discovery)||geteuid()!=0)return 64;
    const char *session=argv[discovery?2:1];char real[PATH_MAX];struct stat st;
    if(strncmp(session,SESSION_ROOT,strlen(SESSION_ROOT))||strlen(session)!=strlen(SESSION_ROOT)+32||
       !realpath(session,real)||strcmp(real,session)||lstat(session,&st)||!S_ISDIR(st.st_mode)||st.st_uid!=0||(st.st_mode&0077))return 64;
    for(const char *p=session+strlen(SESSION_ROOT);*p;p++)if(!((*p>='0'&&*p<='9')||(*p>='a'&&*p<='f')))return 64;
    if(discovery)return discover(session);
    if(!realpath(LOCK_PARENT,real)||strcmp(real,LOCK_PARENT))return 74;
    int parent=open(LOCK_PARENT,O_RDONLY|O_DIRECTORY|O_NOFOLLOW|O_CLOEXEC);
    int directory=open(session,O_RDONLY|O_DIRECTORY|O_NOFOLLOW|O_CLOEXEC);
    if(parent<0||directory<0)return 74;
    int lease=openat(parent,"legacy-recovery.guard",O_RDWR|O_CREAT|O_NOFOLLOW|O_CLOEXEC,0600);
    if(lease<0||fstat(lease,&st)||!S_ISREG(st.st_mode)||st.st_nlink!=1||st.st_uid!=0||(st.st_mode&0077)||flock(lease,LOCK_EX|LOCK_NB))return 75;
    struct fence before,after;
    if(fence_read(parent,&before)||ancestors_capture()||load_targets(session))return 75;
    if(prctl(PR_SET_CHILD_SUBREAPER,1))return 74;
    atexit(detach_all);
    for(int i=0;i<task_count;i++)if(strcmp(tasks[i].role,"preserve-xray")&&pin(&tasks[i]))return 75;
    for(int i=0;i<task_count;i++)if(tasks[i].seconds&&!idle(&tasks[i]))return 75;
    if(!realpath("/opt/broray/run",real)||strcmp(real,"/opt/broray/run"))return 75;
    int run=open("/opt/broray/run",O_RDONLY|O_DIRECTORY|O_NOFOLLOW|O_CLOEXEC);
    if(run<0||projection_capture(run))return 75;
    if(inventory_clear()||run_callback(session,"check")||inventory_clear()||fence_read(parent,&after)||!fence_same(&before,&after))return 75;
    char owner_path[64];snprintf(owner_path,sizeof owner_path,"/proc/%d",before.owner);
    if(!access(owner_path,F_OK)) {
        int owned=0;
        for(int i=0;i<task_count;i++)if(tasks[i].pid==before.owner&&!strncmp(tasks[i].role,"stop-",5))owned=1;
        if(!owned)return 75;
    } else if(errno!=ENOENT)return 75;
    /* Admission sources remain pinned through final bookkeeping. Only this
     * native process can perform the final atomic global-fence retirement. */
    for(int i=0;i<task_count;i++)if(!strncmp(tasks[i].role,"stop-",5)&&terminate_pinned(&tasks[i]))return 75;
    /* Bookkeeping may finish here; it never retires the global fence itself.
     * If this guard dies, descendants can only finish under the retained fence.
     * Successful callback exit also requires absence of adopted descendants. */
    if(run_callback(session,"finalize")||inventory_clear())return 75;
    for(int i=0;i<task_count;i++)if(!tasks[i].stopped&&!same(&tasks[i]))return 75;
    if(fence_read(parent,&after)||!fence_same(&before,&after))return 75;
    if(projection_retire(run,directory))return 75;
    if(fence_sync(parent)||receipt(directory,"quiescent.json","{\"quiescent\":true,\"globalFenceRetired\":false}\n"))return 74;
    if(syscall(SYS_renameat2,parent,"global-operation.lock",directory,"retired-global.lock",1))return 74;
    if(fsync(parent)||fsync(directory))return 74;
    if(receipt(directory,"complete.json","{\"globalFenceRetired\":true,\"prototype\":true}\n"))return 74;
    puts("{\"ok\":true,\"result\":\"legacy_fence_retired\",\"prototype\":true}");
    return 0;
}
