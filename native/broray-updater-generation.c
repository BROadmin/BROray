/* Lifetime updater domain. No adoption of pre-existing processes, no detach,
 * no PID addressed control, no inherited operation coordinator descriptor.
 * This primitive is integrated only after its standalone adversarial gate.
 */
#define _GNU_SOURCE
#include <errno.h>
#include <dirent.h>
#include <fcntl.h>
#include <limits.h>
#include <linux/audit.h>
#include <linux/filter.h>
#include <linux/seccomp.h>
#include <stddef.h>
#include <poll.h>
#include <signal.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/file.h>
#include <sys/inotify.h>
#include <sys/prctl.h>
#include <sys/ptrace.h>
#include <sys/resource.h>
#include <sys/socket.h>
#include <sys/stat.h>
#include <sys/syscall.h>
#include <sys/un.h>
#include <sys/uio.h>
#if defined(__x86_64__)
#include <sys/user.h>
#endif
#include <sys/wait.h>
#include <time.h>
#include <unistd.h>
#include "broray-generation-sha256.h"

#define LIMIT 256
#define SNAPSHOT_LIMIT (LIMIT*(PATH_MAX+512)+16384)
struct identity {pid_t pid;unsigned long long ticks;char exe[PATH_MAX],cmd[65];};
static int platform_run_prepare(int argc,char **argv),platform_run_check(void),platform_request_check(void);
static int platform_ledger_witness(unsigned long nr,const char *bytes,size_t size);
static void platform_exec_root(char **args),platform_state_json(FILE *f,const char *next);
static int platform_control(const struct ucred *peer,const char *verb,const char *op,const char *nonce);
enum sc_action {SC_START,SC_STATUS,SC_CURRENT,SC_STOP,SC_RESTART};
static int service_cycle_main(int argc,char **argv,int action),service_cycle_available(const char *root);
static int replacement_start_prepare(char **argv,int op,const char *op_path,const char *native);
static int replacement_start_main(int argc,char **argv);
static int generation_boot_retirement_valid(int base,const char *domain,const char *manifest_sha,const char *current);
static int service_transition_fd=-1;
static int service_transition_prepare(int argc,char **argv),service_transition_enter(const char *domain);
static void service_transition_release(void);
static struct identity kids[LIMIT],supervisor,updater;
static int awaiting_birth[LIMIT];
static int denied_errno[LIMIT],syscall_info_available;
static struct identity exited[LIMIT];static int exited_count;
static int count,generation_dirfd=-1,lockfd=-1,sockfd=-1,watchfd=-1,root_live,stopping,term_sent,killing,terminal;
static int installation_fd=-1,installation_lock=-1;
static int retire_ready;
static struct stat installation_lock_identity;
static char installation_path[PATH_MAX];
static unsigned long revision;
static char generation[65],manifest[65],operation[97],stop_nonce[65],boot[64],state[32]="START_INTENT";
static char snapshot[SNAPSHOT_LIMIT];static size_t snapshot_size;
static char anchor[SNAPSHOT_LIMIT],latest_name[64],snapshot_hash[65];static size_t anchor_size;
struct trusted_record {char sha[65];size_t size;};
static struct trusted_record *history;static size_t history_capacity,history_cursor;
static int anchor_watch=-1,latest_watch=-1,removed_watch=-1;
static int history_matches_all(void),history_sweep(void);
static uint64_t stop_at,inspection_at;
static volatile sig_atomic_t interrupted;
struct wait_evidence {pid_t pid;int status,index;unsigned long long ticks;};
static struct wait_evidence recent[16];static unsigned recent_at;
static void signal_handler(int sig){(void)sig;interrupted=1;}
static uint64_t millis(void){struct timespec t;if(clock_gettime(CLOCK_MONOTONIC,&t))_exit(74);return (uint64_t)t.tv_sec*1000+t.tv_nsec/1000000;}
static int token(const char *p,size_t max){if(!p||!p[0]||strlen(p)>max)return 0;for(;*p;p++)if(!((*p>='a'&&*p<='z')||(*p>='A'&&*p<='Z')||(*p>='0'&&*p<='9')||*p=='-'||*p=='_'))return 0;return 1;}
static int hex64(const char *p){if(strlen(p)!=64)return 0;for(;*p;p++)if(!((*p>='a'&&*p<='f')||(*p>='0'&&*p<='9')))return 0;return 1;}
static ssize_t read_file(const char *path,void *buf,size_t size){int fd=open(path,O_RDONLY|O_NOFOLLOW|O_CLOEXEC);if(fd<0)return -1;size_t used=0;for(;;){ssize_t n=read(fd,(char*)buf+used,size-used);if(n<0&&errno==EINTR)continue;if(n<0){close(fd);return -1;}if(!n)break;used+=(size_t)n;if(used==size){close(fd);return -1;}}close(fd);return (ssize_t)used;}
static int hash_fd(int fd,char out[65]){struct gen_sha s;gen_sha_init(&s);char b[4096];for(;;){ssize_t n=read(fd,b,sizeof b);if(n<0&&errno==EINTR)continue;if(n<0)return -1;if(!n)break;gen_sha_add(&s,b,(size_t)n);}gen_sha_end(&s,out);return 0;}
static unsigned long long ticks(pid_t pid){char p[64],b[4096];snprintf(p,sizeof p,"/proc/%d/stat",pid);ssize_t n=read_file(p,b,sizeof b-1);if(n<0)return 0;b[n]=0;char *s=strrchr(b,')');if(!s||s[1]!=' ')return 0;s+=2;for(int i=1;i<20;i++){s=strchr(s,' ');if(!s)return 0;s++;}char *end;unsigned long long t=strtoull(s,&end,10);return end==s||(*end!=' '&&*end!='\n')?0:t;}
static int capture(pid_t pid,struct identity *id){char p[64];memset(id,0,sizeof *id);id->pid=pid;id->ticks=ticks(pid);if(!id->ticks)return -1;snprintf(p,sizeof p,"/proc/%d/exe",pid);ssize_t n=readlink(p,id->exe,sizeof id->exe-1);if(n<=0||n>=(ssize_t)sizeof id->exe-1)return -1;id->exe[n]=0;snprintf(p,sizeof p,"/proc/%d/cmdline",pid);int fd=open(p,O_RDONLY|O_CLOEXEC);if(fd<0)return -1;int rc=hash_fd(fd,id->cmd);close(fd);return rc||ticks(pid)!=id->ticks?-1:0;}
static void json_string(FILE *f,const char *p){fputc('"',f);for(;*p;p++){unsigned char c=(unsigned char)*p;if(c=='"'||c=='\\'){fputc('\\',f);fputc(c,f);}else if(c<32||c>=127)fprintf(f,"\\u%04x",c);else fputc(c,f);}fputc('"',f);}
static void identity_json(FILE *f,const struct identity *id){fprintf(f,"{\"pid\":%d,\"startTicks\":\"%llu\",\"bootId\":\"%s\",\"executable\":",id->pid,id->ticks,boot);json_string(f,id->exe);fprintf(f,",\"commandDigest\":\"%s\"}",id->cmd);}
/* Published records are write-once PATHNAMES, not merely immutable inodes.
 * state.json is revision 1. No syscall ever replaces a published name.
 * Detection can race with external corruption; preservation cannot: adding a
 * new name cannot overwrite the corrupt old bytes. The watch covers all past
 * revisions, including deletion/rollback while a new one is being published. */
static int ledger_events(const char *created){unsigned creates=0;union {char bytes[8192];struct inotify_event align;} buf;for(;;){ssize_t n=read(watchfd,buf.bytes,sizeof buf.bytes);if(n<0&&errno==EINTR)continue;if(n<0&&errno==EAGAIN)break;if(n<=0)return -1;for(size_t at=0;at<(size_t)n;){struct inotify_event *e=(void*)(buf.bytes+at);at+=sizeof *e+e->len;if(!e->len&&e->mask==IN_IGNORED&&e->wd==removed_watch){removed_watch=-1;continue;}if(!e->len||e->mask&(IN_Q_OVERFLOW|IN_DELETE_SELF|IN_MOVE_SELF|IN_IGNORED))return -1;if(!strcmp(e->name,"state.json")||!strncmp(e->name,"revision-",9)){if(!created||strcmp(e->name,created)||e->mask!=IN_CREATE)return -1;creates++;}}}return creates==(created?1U:0U)&&removed_watch==-1?0:-1;}
static int watch_record(const char *name){int fd=openat(generation_dirfd,name,O_RDONLY|O_NOFOLLOW|O_CLOEXEC);if(fd<0)return -1;char path[64];snprintf(path,sizeof path,"/proc/self/fd/%d",fd);int wd=inotify_add_watch(watchfd,path,IN_MODIFY|IN_ATTRIB|IN_DELETE_SELF|IN_MOVE_SELF);struct stat held,named;int rc=wd<0||fstat(fd,&held)||fstatat(generation_dirfd,name,&named,AT_SYMLINK_NOFOLLOW)||held.st_dev!=named.st_dev||held.st_ino!=named.st_ino;close(fd);if(rc)return -1;
    if(latest_watch>=0&&latest_watch!=anchor_watch){removed_watch=latest_watch;if(inotify_rm_watch(watchfd,removed_watch)||ledger_events(NULL))return -1;}latest_watch=wd;if(!revision)anchor_watch=wd;return 0;}
static int record_matches(const char *name,const char *data,size_t size){int fd=openat(generation_dirfd,name,O_RDONLY|O_NOFOLLOW|O_CLOEXEC);struct stat st;if(fd<0)return -1;if(fstat(fd,&st)||!S_ISREG(st.st_mode)||st.st_nlink!=1||st.st_uid!=geteuid()||(st.st_mode&07777)!=0600||st.st_size!=(off_t)size){close(fd);return -1;}char b[4096];size_t at=0;while(at<size){size_t need=size-at;if(need>sizeof b)need=sizeof b;ssize_t n=read(fd,b,need);if(n<0&&errno==EINTR)continue;if(n<=0||memcmp(b,data+at,(size_t)n)){close(fd);return -1;}at+=(size_t)n;}close(fd);return 0;}
static int disk_matches(void){if(!snapshot_size)return 0;return record_matches("state.json",anchor,anchor_size)||record_matches(latest_name,snapshot,snapshot_size)?-1:0;}
static int persist(const char *next){if(ledger_events(NULL)||disk_matches()||revision==ULONG_MAX)return -1;
    if(revision>=history_capacity){size_t capacity=history_capacity?history_capacity*2:64;if(capacity<=revision||capacity>SIZE_MAX/sizeof *history)return -1;void *expanded=realloc(history,capacity*sizeof *history);if(!expanded)return -1;history=expanded;history_capacity=capacity;}
    char *data=NULL;size_t size=0;FILE *f=open_memstream(&data,&size);if(!f)return -1;
    fprintf(f,"{\"schemaVersion\":2,\"contract\":\"broray-updater-generation/2\",\"generationId\":\"%s\",\"platformManifestSha256\":\"%s\",\"bootId\":\"%s\",\"revision\":%lu,\"previousRecordSha256\":\"%s\",\"state\":\"%s\",\"supervisedFromBirth\":true,\"supervisor\":",generation,manifest,boot,revision+1,snapshot_hash,next);identity_json(f,&supervisor);fputs(",\"updater\":",f);if(updater.pid)identity_json(f,&updater);else fputs("null",f);
    fprintf(f,",\"stopOperationId\":\"%s\",\"stopNonce\":\"%s\",\"termSent\":%s,\"children\":[",operation,stop_nonce,term_sent?"true":"false");for(int i=0;i<count;i++){if(i)fputc(',',f);identity_json(f,&kids[i]);}fputs("],\"awaitingBirth\":[",f);int sep=0;for(int i=0;i<count;i++)if(awaiting_birth[i]){if(sep++)fputc(',',f);identity_json(f,&kids[i]);}fputs("],\"exitedUnreaped\":[",f);for(int i=0;i<exited_count;i++){if(i)fputc(',',f);identity_json(f,&exited[i]);}fputs("]",f);platform_state_json(f,next);fputs("}\n",f);if(fclose(f)||size>=sizeof snapshot){free(data);return -1;}
    char name[64],pending[64];if(!revision)strcpy(name,"state.json");else snprintf(name,sizeof name,"revision-%020lu.json",revision+1);snprintf(pending,sizeof pending,"pending-%020lu",revision+1);
    int fd=openat(generation_dirfd,pending,O_WRONLY|O_CREAT|O_EXCL|O_NOFOLLOW|O_CLOEXEC,0600);struct stat st;if(fd<0){free(data);return -1;}if(fstat(fd,&st)||!S_ISREG(st.st_mode)||st.st_nlink!=1||st.st_uid!=geteuid()||(st.st_mode&07777)!=0600){close(fd);free(data);return -1;}size_t at=0;while(at<size){ssize_t n=write(fd,data+at,size-at);if(n<0&&errno==EINTR)continue;if(n<=0){close(fd);free(data);return -1;}at+=(size_t)n;}int rc=fsync(fd);if(close(fd))rc=-1;
    /* linkat fails EEXIST, including for symlinks. No replace, no retry, no
     * cleanup on failure: preserve all evidence for explicit recovery. */
    if(rc||record_matches(pending,data,size)||ledger_events(NULL)||disk_matches()||linkat(generation_dirfd,pending,generation_dirfd,name,0)||unlinkat(generation_dirfd,pending,0)||fsync(generation_dirfd)||ledger_events(name)||watch_record(name)||record_matches(name,data,size)||disk_matches()||ledger_events(NULL)){free(data);return -1;}
    if(platform_ledger_witness(revision+1,data,size)){free(data);return -1;}
    if(!revision){memcpy(anchor,data,size);anchor_size=size;}
    memcpy(snapshot,data,size);snapshot_size=size;strcpy(latest_name,name);struct gen_sha digest;gen_sha_init(&digest);gen_sha_add(&digest,data,size);gen_sha_end(&digest,snapshot_hash);strcpy(history[revision].sha,snapshot_hash);history[revision].size=size;free(data);strcpy(state,next);revision++;return 0;
}
static int kid_index(pid_t pid){for(int i=0;i<count;i++)if(kids[i].pid==pid)return i;return -1;}
static int register_kid(pid_t pid,int awaiting){int i=kid_index(pid);struct identity id;if(capture(pid,&id)){fprintf(stderr,"GENERATION_REGISTER_CAPTURE_FAILED pid=%d index=%d ticks=%llu errno=%d\n",pid,i,ticks(pid),errno);return -1;}if(i<0){if(count==LIMIT)return -1;i=count++;denied_errno[i]=0;}else if(kids[i].ticks!=id.ticks)return -1;kids[i]=id;awaiting_birth[i]=awaiting;if(pid==updater.pid)updater=id;int rc=persist(state);if(rc)fprintf(stderr,"GENERATION_REGISTER_PUBLISH_FAILED pid=%d errno=%d\n",pid,errno);return rc;}
/* Only after waitpid returned a held stop of this lifetime-owned tracee.
 * The ptrace relationship and unreaped stop pin the task; a full saved birth,
 * executable and command identity check additionally rejects stale evidence.
 * PTRACE_SYSCALL's signal argument is not an injection mechanism at syscall
 * or PTRACE_EVENT stops. Send a real thread-directed signal while held, then
 * keep tracing/reaping; successful tgkill alone never proves STOPPED. */
static int terminate_stopped_kid(pid_t pid){
    int i=kid_index(pid);struct identity current;
    if(!killing||!stopping||strcmp(state,"DRAINING")||i<0||awaiting_birth[i]||capture(pid,&current)||current.ticks!=kids[i].ticks||strcmp(current.exe,kids[i].exe)||strcmp(current.cmd,kids[i].cmd))return -1;
    char path[64],status[8192],*end;snprintf(path,sizeof path,"/proc/%d/status",pid);ssize_t n=read_file(path,status,sizeof status-1);if(n<=0)return -1;status[n]=0;
    char *field=strstr(status,"\nTgid:");if(!field)return -1;errno=0;long tgid=strtol(field+6,&end,10);
    if(errno||end==field+6||*end!='\n'||tgid<=1||tgid>INT_MAX)return -1;
    return syscall(SYS_tgkill,(pid_t)tgid,pid,SIGKILL)<0&&errno!=ESRCH?-1:0;
}
static void forget_kid(pid_t pid){int i=kid_index(pid);if(i>=0){count--;kids[i]=kids[count];awaiting_birth[i]=awaiting_birth[count];denied_errno[i]=denied_errno[count];}}
/* A tracer receives an exit notification before the actual parent reaps its
 * zombie. If that parent then exits, subreaper receives a second wait event.
 * Retain the exact already-dead birth until reaping; never treat an arbitrary
 * unknown wait result as proof of a formerly registered process. */
static int exited_index(pid_t pid){for(int i=0;i<exited_count;i++)if(exited[i].pid==pid)return i;return -1;}
static int collect_reaped(void){int changed=0;for(int i=0;i<exited_count;){unsigned long long t=ticks(exited[i].pid);if(t==exited[i].ticks){i++;continue;}if(!t){char path[64];struct stat st;snprintf(path,sizeof path,"/proc/%d",exited[i].pid);if(!stat(path,&st)||errno!=ENOENT)return -1;}exited[i]=exited[--exited_count];changed=1;}return changed;}
static int checked_directory(const char *path){if(path[0]!='/')return -1;int fd=open("/",O_RDONLY|O_DIRECTORY|O_CLOEXEC);if(fd<0)return -1;char copy[PATH_MAX];if(strlen(path)>=sizeof copy){close(fd);return -1;}strcpy(copy,path);char *save=NULL;for(char *p=strtok_r(copy,"/",&save);p;p=strtok_r(NULL,"/",&save)){if(!strcmp(p,".")||!strcmp(p,"..")){close(fd);return -1;}int next=openat(fd,p,O_RDONLY|O_DIRECTORY|O_NOFOLLOW|O_CLOEXEC);close(fd);if(next<0)return -1;fd=next;}struct stat st;if(fstat(fd,&st)||st.st_uid!=geteuid()||(st.st_mode&0077)){close(fd);return -1;}return fd;}
/* Sibling generation directories share one installation lifetime lock.
 * Production integration must bind this parent to its canonical installation
 * generation root. This primitive never derives exclusion from a PID. */
static int installation_lock_valid(void){struct stat st;return fstatat(installation_fd,".generation-lifetime.lock",&st,AT_SYMLINK_NOFOLLOW)||!S_ISREG(st.st_mode)||st.st_nlink!=1||st.st_uid!=geteuid()||(st.st_mode&07777)!=0600||st.st_size!=0||st.st_dev!=installation_lock_identity.st_dev||st.st_ino!=installation_lock_identity.st_ino?-1:0;}
static int installation_claim(const char *domain){if(strlen(domain)>=sizeof installation_path)return -1;strcpy(installation_path,domain);char *last=strrchr(installation_path,'/');if(!last||last==installation_path||!token(last+1,64))return -1;*last=0;
    installation_fd=checked_directory(installation_path);if(installation_fd<0)return -1;
    installation_lock=openat(installation_fd,".generation-lifetime.lock",O_RDWR|O_CREAT|O_NOFOLLOW|O_CLOEXEC,0600);
    if(installation_lock<0||fstat(installation_lock,&installation_lock_identity)||installation_lock_valid()||flock(installation_lock,LOCK_EX|LOCK_NB)||fsync(installation_lock)||fsync(installation_fd))return -1;
    return 0;
}
/* Retirement is a write-once receipt, not deletion of a fence or ledger. A
 * successor checks the entire prior record inventory under the shared flock.
 * A crashed/missing/corrupt generation has no valid receipt and blocks it. */
static int safe_bytes_at(int base,const char *name,char **out,size_t *size){
    int fd=openat(base,name,O_RDONLY|O_NOFOLLOW|O_CLOEXEC);struct stat st;
    if(fd<0)return -1;
    if(fstat(fd,&st)||!S_ISREG(st.st_mode)||st.st_uid!=geteuid()||st.st_nlink!=1||(st.st_mode&07777)!=0600||st.st_size<0||st.st_size>=SNAPSHOT_LIMIT){close(fd);return -1;}
    size_t n=(size_t)st.st_size;char *b=malloc(n+1);if(!b){close(fd);return -1;}
    size_t at=0;while(at<n){ssize_t got=read(fd,b+at,n-at);if(got<0&&errno==EINTR)continue;if(got<=0){free(b);close(fd);return -1;}at+=(size_t)got;}
    char extra;if(read(fd,&extra,1)!=0){free(b);close(fd);return -1;}close(fd);b[n]=0;*out=b;*size=n;return 0;
}
static void digest_bytes(const char *data,size_t n,char out[65]){struct gen_sha h;gen_sha_init(&h);gen_sha_add(&h,data,n);gen_sha_end(&h,out);}
static DIR *directory_stream(int base){int fd=openat(base,".",O_RDONLY|O_DIRECTORY|O_NOFOLLOW|O_CLOEXEC);if(fd<0)return NULL;DIR *d=fdopendir(fd);if(!d)close(fd);return d;}
static int empty_directory(int base){DIR *d=directory_stream(base);if(!d)return -1;int empty=1;struct dirent *e;errno=0;while((e=readdir(d))){if(strcmp(e->d_name,".")&&strcmp(e->d_name,"..")){empty=0;break;}}if(!e&&errno)empty=-1;closedir(d);return empty;}
static void record_name(unsigned long n,char name[64]){if(n==1)strcpy(name,"state.json");else snprintf(name,64,"revision-%020lu.json",n);}
/* Historical records remain write-once. Cache only their exact publication
 * hashes, never trust a disk projection as healthy. Directory notifications
 * catch normal historical mutations; hash sweeps also catch mmap and changes
 * through an external hardlink. Every control/terminal boundary verifies the
 * COMPLETE history. Watch count is constant, independent of lifetime forks. */
static int history_record_matches(size_t index){char name[64],actual[65],*bytes=NULL;size_t n;if(index>=revision)return -1;record_name(index+1,name);if(safe_bytes_at(generation_dirfd,name,&bytes,&n))return -1;digest_bytes(bytes,n,actual);free(bytes);return n!=history[index].size||strcmp(actual,history[index].sha)?-1:0;}
static int history_matches_all(void){for(size_t i=0;i<revision;i++)if(history_record_matches(i))return -1;return ledger_events(NULL)||disk_matches()?-1:0;}
static int history_sweep(void){size_t batch=revision<8?revision:8;for(size_t i=0;i<batch;i++){if(history_cursor>=revision)history_cursor=0;if(history_record_matches(history_cursor++))return -1;}return 0;}
static int ledger_inventory(int base,unsigned long total,char hash[65]){
    if(!total||total>1000000)return -1;
    DIR *d=directory_stream(base);if(!d)return -1;struct dirent *e;unsigned long found=0;int rc=0;errno=0;
    while((e=readdir(d))){const char *name=e->d_name;
        if(!strcmp(name,".")||!strcmp(name,"..")||!strcmp(name,"control")||!strcmp(name,"lifetime.lock")||!strcmp(name,"retirement.receipt"))continue;
        if(!strcmp(name,"state.json")){found++;continue;}
        unsigned long nr=0;char tail,expected[64];
        if(sscanf(name,"revision-%20lu.json%c",&nr,&tail)!=1||nr<2||nr>total){rc=-1;break;}
        record_name(nr,expected);if(strcmp(name,expected)){rc=-1;break;}found++;
    }
    if(!e&&errno)rc=-1;closedir(d);if(rc||found!=total)return -1;
    struct gen_sha all;gen_sha_init(&all);
    for(unsigned long nr=1;nr<=total;nr++){char name[64],digest[65],*b=NULL;size_t n;record_name(nr,name);if(safe_bytes_at(base,name,&b,&n))return -1;digest_bytes(b,n,digest);free(b);gen_sha_add(&all,name,strlen(name)+1);gen_sha_add(&all,digest,64);}
    gen_sha_end(&all,hash);return 0;
}
struct retired_record {char gen[65],sha[65],op[97],nonce[65],inventory[65],last[65],scope[65];unsigned long total;};
static int retirement_text(const struct retired_record *r,char *out,size_t size){return snprintf(out,size,"BROray-generation-retired/1\n%s\n%s\n%s\n%s\n%lu\n%s\n%s\n%s\n",r->gen,r->sha,r->op,r->nonce,r->total,r->inventory,r->last,r->scope);}
static void scope_digest(const char *path,char out[65]){digest_bytes(path,strlen(path),out);}
static int retirement_valid(int base,const char *path,struct retired_record *result){
    char *text=NULL;size_t n;if(safe_bytes_at(base,"retirement.receipt",&text,&n))return -1;
    struct retired_record r;memset(&r,0,sizeof r);char extra,canonical[1024],actual[65],*last=NULL;size_t last_size;
    int fields=sscanf(text,"BROray-generation-retired/1\n%64s\n%64s\n%96s\n%64s\n%lu\n%64s\n%64s\n%64s\n%c",r.gen,r.sha,r.op,r.nonce,&r.total,r.inventory,r.last,r.scope,&extra);
    int written=retirement_text(&r,canonical,sizeof canonical);int bad=fields!=8||written<0||(size_t)written!=n||memcmp(text,canonical,n)||!token(r.gen,64)||!hex64(r.sha)||!token(r.op,96)||!token(r.nonce,64)||!hex64(r.inventory)||!hex64(r.last)||!hex64(r.scope);free(text);if(bad)return -1;
    scope_digest(path,actual);if(strcmp(actual,r.scope)||ledger_inventory(base,r.total,actual)||strcmp(actual,r.inventory))return -1;
    char name[64];record_name(r.total,name);if(safe_bytes_at(base,name,&last,&last_size))return -1;digest_bytes(last,last_size,actual);
    char gen_field[96],sha_field[112],op_field[132],nonce_field[96];snprintf(gen_field,sizeof gen_field,"\"generationId\":\"%s\"",r.gen);snprintf(sha_field,sizeof sha_field,"\"platformManifestSha256\":\"%s\"",r.sha);snprintf(op_field,sizeof op_field,"\"stopOperationId\":\"%s\"",r.op);snprintf(nonce_field,sizeof nonce_field,"\"stopNonce\":\"%s\"",r.nonce);
    bad=strcmp(actual,r.last)||!strstr(last,"\"state\":\"STOPPED\"")||!strstr(last,"\"children\":[]")||!strstr(last,"\"awaitingBirth\":[]")||!strstr(last,"\"exitedUnreaped\":[]")||!strstr(last,gen_field)||!strstr(last,sha_field)||!strstr(last,op_field)||!strstr(last,nonce_field);free(last);if(bad)return -1;if(result)*result=r;return 0;
}
static int installation_history_valid(const char *current){
    DIR *d=directory_stream(installation_fd);if(!d)return -1;struct dirent *e;int rc=0;errno=0;
    while((e=readdir(d))){if(!strcmp(e->d_name,".")||!strcmp(e->d_name,".."))continue;struct stat st;if(fstatat(installation_fd,e->d_name,&st,AT_SYMLINK_NOFOLLOW)){rc=-1;break;}if(!S_ISDIR(st.st_mode))continue;
        char path[PATH_MAX];if(!token(e->d_name,64)||snprintf(path,sizeof path,"%s/%s",installation_path,e->d_name)>=(int)sizeof path){rc=-1;break;}
        int fd=checked_directory(path);if(fd<0){rc=-1;break;}int empty=empty_directory(fd);
        if(!strcmp(path,current)){if(empty!=1)rc=-1;}else if(empty<0)rc=-1;
        else if(!empty){struct retired_record prior;if(retirement_valid(fd,path,&prior)){if(generation_boot_retirement_valid(fd,path,manifest,generation))rc=-1;}else if(!strcmp(prior.gen,generation))rc=-1;}
        close(fd);if(rc)break;errno=0;
    }
    if(!e&&errno)rc=-1;closedir(d);return rc;
}
static int retire_generation(const char *domain){
    if(!terminal||strcmp(state,"STOPPED")||count||exited_count||root_live||!token(operation,96)||!token(stop_nonce,64)||installation_lock_valid()||history_matches_all())return -1;
    struct retired_record r;memset(&r,0,sizeof r);strcpy(r.gen,generation);strcpy(r.sha,manifest);strcpy(r.op,operation);strcpy(r.nonce,stop_nonce);r.total=revision;strcpy(r.last,snapshot_hash);scope_digest(domain,r.scope);
    if(ledger_inventory(generation_dirfd,revision,r.inventory)||ledger_events(NULL)||disk_matches())return -1;
    char b[1024];int n=retirement_text(&r,b,sizeof b);if(n<=0||n>=(int)sizeof b)return -1;
    int fd=openat(generation_dirfd,"retirement.pending",O_WRONLY|O_CREAT|O_EXCL|O_NOFOLLOW|O_CLOEXEC,0600);if(fd<0)return -1;size_t at=0;while(at<(size_t)n){ssize_t got=write(fd,b+at,(size_t)n-at);if(got<0&&errno==EINTR)continue;if(got<=0){close(fd);return -1;}at+=(size_t)got;}
    int rc=fsync(fd);if(close(fd))rc=-1;
    if(rc||record_matches("retirement.pending",b,(size_t)n)||ledger_events(NULL)||disk_matches()||linkat(generation_dirfd,"retirement.pending",generation_dirfd,"retirement.receipt",0)||unlinkat(generation_dirfd,"retirement.pending",0)||fsync(generation_dirfd)||record_matches("retirement.receipt",b,(size_t)n)||retirement_valid(generation_dirfd,domain,NULL)||ledger_events(NULL)||disk_matches())return -1;
    retire_ready=1;return 0;
}
static char domain_path[PATH_MAX];
static int make_socket(const char *base){struct sockaddr_un addr;memset(&addr,0,sizeof addr);addr.sun_family=AF_UNIX;if(snprintf(addr.sun_path,sizeof addr.sun_path,"%s/control",base)>=(int)sizeof addr.sun_path)return -1;int fd=socket(AF_UNIX,SOCK_SEQPACKET|SOCK_CLOEXEC|SOCK_NONBLOCK,0);if(fd<0)return -1;if(bind(fd,(void*)&addr,sizeof addr)||listen(fd,4)){close(fd);return -1;}return fd;}
static int request(int fd){struct ucred peer;socklen_t size=sizeof peer;if(getsockopt(fd,SOL_SOCKET,SO_PEERCRED,&peer,&size)||peer.uid!=geteuid())return -1;char b[512],verb[16],gen[65],sha[65],op[97],nonce[65],tail;ssize_t n=recv(fd,b,sizeof b-1,MSG_DONTWAIT);if(n<=0)return -1;b[n]=0;
    int fields=sscanf(b,"%15s %64s %64s %96s %64s %c",verb,gen,sha,op,nonce,&tail);if(fields!=5||strcmp(gen,generation)||strcmp(sha,manifest)||!token(op,96)||!token(nonce,64))return -1;
    if(history_matches_all()||platform_request_check())return -2;
    if(!strcmp(verb,"STOP")){
        if(stopping){
            /* An unexpected root exit drains its whole writer domain before
             * any recovery request exists. Bind that ALREADY verified terminal
             * proof once; do not re-signal or reinterpret an unfinished drain. */
            if(terminal&&!operation[0]&&!stop_nonce[0]&&!count&&!exited_count&&!root_live&&!strcmp(state,"STOPPED")){
                strcpy(operation,op);strcpy(stop_nonce,nonce);if(persist("STOPPED"))return -2;
            }else if(strcmp(op,operation)||strcmp(nonce,stop_nonce))return -1;
        }else{strcpy(operation,op);strcpy(stop_nonce,nonce);if(persist("STOP_INTENT"))return -2;stopping=1;stop_at=millis();}
    }
    else if(!strcmp(verb,"RETIRE")){if(!terminal||strcmp(op,operation)||strcmp(nonce,stop_nonce))return -1;if(retire_generation(domain_path))return -2;}
    else if(!strcmp(verb,"AUTH")||!strcmp(verb,"READY")){int rc=platform_control(&peer,verb,op,nonce);if(rc<0)return rc;if(rc&&persist(state))return -2;}
    else if(strcmp(verb,"STATUS"))return -1;
    if(ledger_events(NULL)||disk_matches())return -2;
    if(send(fd,snapshot,snapshot_size,MSG_NOSIGNAL)!=(ssize_t)snapshot_size)return -1;return 0;
}
/* A generation child authenticates this supervisor before sending its packet.
 * Blocking here prevents that child's ptrace syscall stops from progressing.
 * CP223 measured 198 ms for the exact authentication under ptrace: the old
 * 100 ms receive bound expired before a valid sender could send. Use the same
 * two-second I/O bound as the authenticated client and independent service
 * channel, with bounded descriptors and no retry. Keep dispatching tracees. */
#define CONTROL_PENDING_LIMIT 4
#define CONTROL_IO_MILLISECONDS 2000U
static struct pending_control {int fd;uint64_t deadline;} pending_control[CONTROL_PENDING_LIMIT];
static int control_pump(void){
    int client=accept4(sockfd,NULL,NULL,SOCK_CLOEXEC|SOCK_NONBLOCK);
    if(client>=0){int slot=-1;for(int i=0;i<CONTROL_PENDING_LIMIT;i++)if(pending_control[i].fd<0){slot=i;break;}
        if(slot<0)close(client);else pending_control[slot]=(struct pending_control){client,millis()+CONTROL_IO_MILLISECONDS};}
    for(int i=0;i<CONTROL_PENDING_LIMIT;i++){
        struct pending_control *p=&pending_control[i];if(p->fd<0)continue;
        char first;ssize_t got=recv(p->fd,&first,1,MSG_PEEK|MSG_DONTWAIT);
        if(got<0&&(errno==EAGAIN||errno==EWOULDBLOCK||errno==EINTR)&&millis()<p->deadline)continue;
        if(got<0&&(errno==EAGAIN||errno==EWOULDBLOCK)&&millis()>=p->deadline)
            fprintf(stderr,"GENERATION_CONTROL_PACKET_TIMEOUT elapsed_ms=%llu\n",(unsigned long long)(millis()-(p->deadline-CONTROL_IO_MILLISECONDS)));
        int rc=got>0?request(p->fd):-1;if(rc<0)send(p->fd,"REFUSED\n",8,MSG_NOSIGNAL);
        close(p->fd);p->fd=-1;if(rc==-2)return -2;
    }
    return 0;
}
static int fail(const char *reason){fprintf(stderr,"GENERATION_FIRST_ERROR=%s\n",reason);unsigned first=recent_at>16?recent_at-16:0;for(unsigned n=first;n<recent_at;n++){struct wait_evidence *e=&recent[n%16];fprintf(stderr,"GENERATION_WAIT pid=%d status=%x index=%d observedTicks=%llu\n",e->pid,e->status,e->index,e->ticks);}return 74;}
/* All updater syscalls remain ptrace-owned. Kernel entry stops precede clone,
 * so an updater cannot opt out using CLONE_UNTRACED. Deny clone3 rather than
 * racing a pointer-valued flags structure. No seccomp dependency on Keenetic.
 * Modern kernels expose GET_SYSCALL_INFO. ARM64 Linux 4.9 uses its documented
 * x7 entry/exit marker and NT_ARM_SYSTEM_CALL; verified on the physical target.
 */
struct call_info {unsigned char op,pad[3];unsigned int arch;unsigned long long ip,sp;union {struct {unsigned long long nr,args[6];} entry;struct {long long value;unsigned char error;} exit;} u;};
#if defined(__aarch64__)
struct arm_regs {uint64_t x[31],sp,pc,pstate;};
#endif
static int syscall_guard(pid_t pid){
    int i=kid_index(pid),entry=-1;unsigned long long nr=0,flags=0;
    if(i<0)return -1;
    struct call_info info;memset(&info,0,sizeof info);
    if(syscall_info_available>=0){long got=ptrace(0x420e,pid,sizeof info,&info);if(got>=0){
#if defined(__aarch64__)
        if(info.arch!=AUDIT_ARCH_AARCH64)return -1;
#else
        if(info.arch!=AUDIT_ARCH_X86_64)return -1;
#endif
        if(info.op==1){entry=1;nr=info.u.entry.nr;flags=info.u.entry.args[0];}else if(info.op==2)entry=0;else return -1;syscall_info_available=1;
    }else{if(errno!=EINVAL&&errno!=EIO)return -1;syscall_info_available=-1;}}
#if defined(__aarch64__)
    struct arm_regs regs;struct iovec io={&regs,sizeof regs};
    if(ptrace(PTRACE_GETREGSET,pid,(void*)1,&io)||io.iov_len!=sizeof regs||(regs.pstate&0x10))return -1;
    if(entry<0){int call;struct iovec number={&call,sizeof call};if(ptrace(PTRACE_GETREGSET,pid,(void*)0x404,&number)||number.iov_len!=sizeof call||regs.x[7]>1)return -1;entry=regs.x[7]==0;nr=(unsigned int)call;flags=regs.x[0];}
#else
    if(entry<0)return -1; /* Test host needs GET_SYSCALL_INFO; never guess phase. */
    struct user_regs_struct regs;
#endif
    if(entry){
        if(denied_errno[i])return -1;
        int deny=nr==__NR_clone3?ENOSYS:(nr==__NR_clone&&(flags&0x00800000)?EPERM:0);
        if(!deny)return 0;
#if defined(__aarch64__)
        int invalid=-1;struct iovec number={&invalid,sizeof invalid};if(ptrace(PTRACE_SETREGSET,pid,(void*)0x404,&number))return -1;
#else
        if(ptrace(PTRACE_GETREGS,pid,0,&regs))return -1;regs.orig_rax=(unsigned long long)-1;if(ptrace(PTRACE_SETREGS,pid,0,&regs))return -1;
#endif
        denied_errno[i]=deny;
    }else if(denied_errno[i]){
#if defined(__aarch64__)
        regs.x[0]=(uint64_t)-(long)denied_errno[i];if(ptrace(PTRACE_SETREGSET,pid,(void*)1,&io))return -1;
#else
        if(ptrace(PTRACE_GETREGS,pid,0,&regs))return -1;regs.rax=(unsigned long long)-(long)denied_errno[i];if(ptrace(PTRACE_SETREGS,pid,0,&regs))return -1;
#endif
        denied_errno[i]=0;
    }
    return 0;
}
static int run_generation(int argc,char **argv){/* run PRIVATE_DIR GEN SHA -- absolute command... */
    if(argc<8||strcmp(argv[5],"--")||!token(argv[3],64)||!hex64(argv[4])||argv[6][0]!='/')return 64;
    strcpy(generation,argv[3]);strcpy(manifest,argv[4]);umask(077);
    if(service_transition_prepare(argc,argv))return fail("SERVICE_TRANSITION_UNCONFIRMED");
    /* No coordinator descriptor survives; only the verified transition gate
     * may bridge admission, and is closed before birthing the updater. */
    struct rlimit limit;if(getrlimit(RLIMIT_NOFILE,&limit))return 74;rlim_t last=limit.rlim_cur;if(last>1048576)last=1048576;for(int fd=3;fd<(int)last;fd++)if(fd!=service_transition_fd)close(fd);
    signal(SIGPIPE,SIG_IGN);signal(SIGCHLD,SIG_DFL);signal(SIGTERM,signal_handler);signal(SIGINT,signal_handler);signal(SIGHUP,signal_handler);
    if(platform_run_prepare(argc,argv))return fail("PLATFORM_LAUNCH_UNCONFIRMED");
    generation_dirfd=checked_directory(argv[2]);if(generation_dirfd<0)return fail("GENERATION_DIRECTORY_UNSAFE");
    if(service_transition_enter(argv[2]))return fail("SERVICE_TRANSITION_BUSY");
    if(installation_claim(argv[2]))return fail("GENERATION_ALREADY_OWNED");
    if(installation_history_valid(argv[2]))return fail("GENERATION_REQUIRES_EXPLICIT_RECOVERY");
    strcpy(domain_path,argv[2]);
    lockfd=openat(generation_dirfd,"lifetime.lock",O_RDWR|O_CREAT|O_NOFOLLOW|O_CLOEXEC,0600);struct stat st;if(lockfd<0||fstat(lockfd,&st)||!S_ISREG(st.st_mode)||st.st_nlink!=1||(st.st_mode&0077)||st.st_uid!=geteuid()||flock(lockfd,LOCK_EX|LOCK_NB))return fail("GENERATION_ALREADY_OWNED");
    if(fstatat(generation_dirfd,"state.json",&st,AT_SYMLINK_NOFOLLOW)==0||errno!=ENOENT)return fail("GENERATION_REQUIRES_EXPLICIT_RECOVERY");
    watchfd=inotify_init1(IN_NONBLOCK|IN_CLOEXEC);if(watchfd<0||inotify_add_watch(watchfd,argv[2],IN_MODIFY|IN_ATTRIB|IN_CREATE|IN_DELETE|IN_MOVED_FROM|IN_MOVED_TO|IN_DELETE_SELF|IN_MOVE_SELF)<0)return fail("LEDGER_WATCH_UNAVAILABLE");
    ssize_t n=read_file("/proc/sys/kernel/random/boot_id",boot,sizeof boot-1);if(n<1)return 74;boot[n]=0;if(boot[n-1]=='\n')boot[n-1]=0;if(!token(boot,63)||capture(getpid(),&supervisor))return 74;
    if(prctl(PR_SET_CHILD_SUBREAPER,1,0,0,0)||persist("START_INTENT"))return fail("START_INTENT_NOT_DURABLE");
    sockfd=make_socket(argv[2]);if(sockfd<0)return fail("CONTROL_SOCKET_UNAVAILABLE");
    service_transition_release();
    for(int i=0;i<CONTROL_PENDING_LIMIT;i++)pending_control[i].fd=-1;
    /* Fresh generation-owned pipe, not an inherited coordinator descriptor.
     * Both ends survive only in the tracee tree. The shell can use a builtin
     * timed read without creating perpetual sleep children and ledger files. */
    int gate[2],idle[2];if(pipe2(gate,O_CLOEXEC)||pipe2(idle,O_CLOEXEC))return 74;pid_t expected_parent=getpid(),root=fork();if(root<0)return 74;
    if(root==0){close(gate[1]);if(prctl(PR_SET_PDEATHSIG,SIGKILL,0,0,0)||getppid()!=expected_parent)_exit(74);char c;if(read(gate[0],&c,1)!=1||c!='G')_exit(74);close(gate[0]);signal(SIGTERM,SIG_DFL);signal(SIGINT,SIG_DFL);signal(SIGHUP,SIG_DFL);signal(SIGPIPE,SIG_DFL);
        char readfd[24],writefd[24];snprintf(readfd,sizeof readfd,"%d",idle[0]);snprintf(writefd,sizeof writefd,"%d",idle[1]);
        if(fcntl(idle[0],F_SETFD,0)<0||fcntl(idle[1],F_SETFD,0)<0||setenv("BRORAY_UPDATER_IDLE_READ_FD",readfd,1)||setenv("BRORAY_UPDATER_IDLE_WRITE_FD",writefd,1)||setenv("BRORAY_UPDATER_GENERATION",generation,1)||setenv("BRORAY_UPDATER_GENERATION_ROOT",argv[2],1))_exit(74);
        platform_exec_root(argv+6);_exit(127);}
    close(idle[0]);close(idle[1]);close(gate[0]);root_live=1;updater.pid=root;
    long opts=PTRACE_O_EXITKILL|PTRACE_O_TRACESYSGOOD|PTRACE_O_TRACEFORK|PTRACE_O_TRACEVFORK|PTRACE_O_TRACECLONE|PTRACE_O_TRACEEXEC|PTRACE_O_TRACEEXIT;
    if(ptrace(PTRACE_SEIZE,root,0,opts)<0){close(gate[1]);return fail("BIRTH_TRACE_UNSUPPORTED");}
    int birth_status;if(ptrace(PTRACE_INTERRUPT,root,0,0)<0||waitpid(root,&birth_status,__WALL)!=root||!WIFSTOPPED(birth_status)){close(gate[1]);return fail("BIRTH_SYSCALL_GATE_UNCONFIRMED");}
    if(register_kid(root,0)||persist("STARTING")){close(gate[1]);return fail("BIRTH_LEDGER_UNCONFIRMED");}
    if(ptrace(PTRACE_SYSCALL,root,0,0)<0){close(gate[1]);return fail("BIRTH_SYSCALL_TRACE_UNSUPPORTED");}
    if(write(gate[1],"G",1)!=1){close(gate[1]);return 74;}close(gate[1]);
    sigset_t child_events;sigemptyset(&child_events);sigaddset(&child_events,SIGCHLD);if(sigprocmask(SIG_BLOCK,&child_events,NULL))return 74;
    for(;;){
        if(installation_lock_valid())return fail("INSTALLATION_EXCLUSION_CHANGED");
        if(platform_run_check())return fail("PLATFORM_LAUNCH_BYTES_CHANGED");
        if(ledger_events(NULL))return fail("GENERATION_LEDGER_CHANGED");
        /* Anchor/current inodes and the directory are watched. Preserve a
         * periodic exact read/hash sweep as well, and unconditional reads at
         * publication/control boundaries. Re-reading snapshots at EVERY
         * syscall stop made the tracer itself the dominant I/O workload. */
        uint64_t now=millis();if(now>=inspection_at){if(disk_matches()||history_sweep())return fail("GENERATION_LEDGER_CHANGED");inspection_at=now+100;}
        if(interrupted)return fail("SUPERVISOR_INTERRUPTED");
        int collected=collect_reaped();if(collected<0)return fail("EXIT_REAP_UNCONFIRMED");if(collected&&persist(state))return fail("REAP_LEDGER_FAILED");
        if(stopping&&!terminal&&count==0&&exited_count==0){if(history_matches_all())return fail("TERMINAL_LEDGER_UNCONFIRMED");terminal=1;if(persist("STOPPED"))return fail("STOPPED_NOT_DURABLE");}
        if(control_pump()==-2)return fail("STOP_INTENT_NOT_DURABLE");
        if(retire_ready)return 0;
        if(stopping&&!term_sent&&!terminal){/* Direct, unreaped fork child; never a PID lookup. */
            term_sent=1;if(persist("STOPPING"))return fail("TERM_INTENT_NOT_DURABLE");
            if(root_live&&kill(root,SIGTERM)<0&&errno!=ESRCH)return fail("ROOT_TERM_FAILED");
        }
        if(stopping&&!killing&&!terminal&&millis()-stop_at>=1500){killing=1;if(persist("DRAINING"))return fail("DRAIN_INTENT_NOT_DURABLE");for(int i=0;i<count;i++)if(ptrace(PTRACE_INTERRUPT,kids[i].pid,0,0)<0&&errno!=ESRCH)return fail("TRACE_INTERRUPT_FAILED");}
        int status;pid_t pid=waitpid(-1,&status,__WALL|WNOHANG);
        if(pid<0&&errno!=ECHILD&&errno!=EINTR)return fail("TRACE_WAIT_FAILED");
        if(pid<=0){struct timespec pause={0,10000000L};sigtimedwait(&child_events,NULL,&pause);continue;}
        if(!WIFSTOPPED(status)||WSTOPSIG(status)!=(SIGTRAP|0x80))recent[recent_at++%16]=(struct wait_evidence){pid,status,kid_index(pid),ticks(pid)};
        if(WIFEXITED(status)||WIFSIGNALED(status)){
            int i=kid_index(pid);if(i<0){int dead=exited_index(pid);if(dead<0)return fail("UNREGISTERED_EXIT");exited[dead]=exited[--exited_count];}
            else{if(ticks(pid)==kids[i].ticks){if(exited_count==LIMIT)return fail("EXIT_LEDGER_FULL");exited[exited_count++]=kids[i];}forget_kid(pid);}
            if(pid==root){root_live=0;if(!stopping){stopping=1;stop_at=millis();}}
            if(persist(state))return fail("EXIT_LEDGER_FAILED");continue;
        }
        if(!WIFSTOPPED(status))return fail("UNKNOWN_WAIT_EVENT");
        unsigned event=(unsigned)status>>16;int deliver=0;
        /* Kernel may report the child's initial stop before the parent's fork
         * event. Hold that stop until parent linkage is consumed. Resuming it
         * early allowed a short-lived child to exit before later registration. */
        if(kid_index(pid)<0){if(event!=PTRACE_EVENT_STOP||register_kid(pid,1))return fail("BIRTH_REGISTRATION_FAILED");continue;}
        if(event==PTRACE_EVENT_FORK||event==PTRACE_EVENT_VFORK||event==PTRACE_EVENT_CLONE){unsigned long born=0;if(ptrace(PTRACE_GETEVENTMSG,pid,0,&born)||born>INT_MAX)return fail("DESCENDANT_ID_UNCONFIRMED");int idx=kid_index((pid_t)born),held=idx>=0&&awaiting_birth[idx];if(register_kid((pid_t)born,0))return fail("DESCENDANT_REGISTRATION_FAILED");if(held){if(killing&&terminate_stopped_kid((pid_t)born))return fail("BIRTH_TERMINATION_UNCONFIRMED");if(ptrace(PTRACE_SYSCALL,(pid_t)born,0,0)<0&&errno!=ESRCH)return fail("BIRTH_RELEASE_FAILED");}}
        else if(event==PTRACE_EVENT_EXEC){unsigned long former=0;if(ptrace(PTRACE_GETEVENTMSG,pid,0,&former))return fail("EXEC_IDENTITY_FAILED");if(former&&(pid_t)former!=pid)forget_kid((pid_t)former);if(register_kid(pid,0))return fail("EXEC_LEDGER_FAILED");if(pid==root&&!stopping&&persist("RUNNING"))return fail("RUNNING_NOT_DURABLE");}
        else if(event==PTRACE_EVENT_STOP&&WSTOPSIG(status)!=SIGTRAP&&!killing){if(ptrace(PTRACE_LISTEN,pid,0,0)<0&&errno!=ESRCH)return fail("TRACE_LISTEN_FAILED");continue;}
        else if(event==0&&WSTOPSIG(status)==(SIGTRAP|0x80)){if(syscall_guard(pid))return fail("SYSCALL_CONTAINMENT_UNCONFIRMED");}
        else if(event==0)deliver=WSTOPSIG(status);
        if(killing){if(event!=PTRACE_EVENT_EXIT&&terminate_stopped_kid(pid))return fail("TRACE_TERMINATION_UNCONFIRMED");deliver=0;}
        if(ptrace(PTRACE_SYSCALL,pid,0,deliver)<0&&errno!=ESRCH)return fail("TRACE_CONTINUE_FAILED");
    }
}
static int retired_reply(char **argv,int emit){
    /* A durable terminal receipt permits only an exact RETIRE replay. It is
     * never readiness, a live-owner status, or authorization for a signal. */
    if(strcmp(argv[3],"RETIRE"))return 75;
    int fd=checked_directory(argv[2]);if(fd<0)return 75;struct retired_record r;
    if(retirement_valid(fd,argv[2],&r)||strcmp(r.gen,argv[4])||strcmp(r.sha,argv[5])||strcmp(r.op,argv[6])||strcmp(r.nonce,argv[7])){close(fd);return 75;}
    char name[64],digest[65],*bytes=NULL;size_t n;record_name(r.total,name);
    if(safe_bytes_at(fd,name,&bytes,&n)){close(fd);return 75;}close(fd);digest_bytes(bytes,n,digest);
    if(strcmp(digest,r.last)||n>=sizeof snapshot||memchr(bytes,0,n)){free(bytes);return 75;}
    memcpy(snapshot,bytes,n);snapshot[n]=0;int rc=!emit||fwrite(bytes,1,n,stdout)==n?0:74;free(bytes);return rc;
}
/* Authenticate the socket server BEFORE sending a mutating verb. SO_PEERCRED
 * binds the connection to a task, not to a trusted implementation or domain.
 * The exact executable bytes, birth and argv bind this client to the native
 * generation for the requested directory/manifest. Cross-version integration
 * must invoke the authenticated matching client; no digest from a reply is
 * accepted as a trust root. */
static int identity_equal(const struct identity *a,const struct identity *b){return a->pid==b->pid&&a->ticks==b->ticks&&!strcmp(a->exe,b->exe)&&!strcmp(a->cmd,b->cmd);}
static int peer_executable_hash(pid_t pid,char out[65]){
    char path[64];snprintf(path,sizeof path,"/proc/%d/exe",pid);
    /* /proc/PID/exe is the kernel's held executable link, intentionally followed. */
    int fd=open(path,O_RDONLY|O_CLOEXEC);struct stat st;if(fd<0)return -1;
    int rc=fstat(fd,&st)||!S_ISREG(st.st_mode)||st.st_uid!=geteuid()||st.st_nlink!=1||(st.st_mode&0022)?-1:hash_fd(fd,out);close(fd);return rc;
}
static int control_peer(int fd,char **argv,struct identity *id){
    struct ucred peer;socklen_t size=sizeof peer;char own[65],remote[65],path[64],cmd[65536];struct identity again;
    if(getsockopt(fd,SOL_SOCKET,SO_PEERCRED,&peer,&size)||size!=sizeof peer||peer.pid<=1||peer.uid!=geteuid()||capture(peer.pid,id))return -1;
    if(peer_executable_hash(getpid(),own)||peer_executable_hash(peer.pid,remote)||strcmp(own,remote))return -1;
    snprintf(path,sizeof path,"/proc/%d/cmdline",peer.pid);ssize_t n=read_file(path,cmd,sizeof cmd);if(n<=0||cmd[n-1]!=0)return -1;
    char digest[65];digest_bytes(cmd,(size_t)n,digest);if(strcmp(digest,id->cmd))return -1;
    const char *expected[]={NULL,"run",argv[2],argv[4],argv[5],"--"};size_t offset=0;
    for(unsigned i=0;i<sizeof expected/sizeof expected[0];i++){
        if(offset>=(size_t)n)return -1;size_t len=strlen(cmd+offset);
        if(!len||(expected[i]&&strcmp(cmd+offset,expected[i])))return -1;offset+=len+1;
    }
    if(offset>=(size_t)n||cmd[offset]!='/')return -1;
    if(capture(peer.pid,&again)||!identity_equal(id,&again))return -1;return 0;
}
static int control_response(char **argv,const struct identity *peer,size_t n){
    char prefix[384],name[64],*end;int length=snprintf(prefix,sizeof prefix,"{\"schemaVersion\":2,\"contract\":\"broray-updater-generation/2\",\"generationId\":\"%s\",\"platformManifestSha256\":\"%s\",\"bootId\":\"%s\",\"revision\":",argv[4],argv[5],boot);
    if(length<=0||(size_t)length>=n||memcmp(snapshot,prefix,(size_t)length))return -1;
    errno=0;unsigned long nr=strtoul(snapshot+length,&end,10);if(errno||!nr||end==snapshot+length||*end!=','||nr>1000000)return -1;
    char *identity=NULL;size_t identity_size=0;FILE *f=open_memstream(&identity,&identity_size);if(!f)return -1;
    fputs("\"supervisor\":",f);identity_json(f,peer);fputc(',',f);if(fclose(f)){free(identity);return -1;}
    int bad=!strstr(snapshot,identity);free(identity);if(bad)return -1;
    int saved=generation_dirfd;generation_dirfd=checked_directory(argv[2]);if(generation_dirfd<0){generation_dirfd=saved;return -1;}
    record_name(nr,name);int rc=record_matches(name,snapshot,n);close(generation_dirfd);generation_dirfd=saved;return rc;
}
static int control_exchange(int argc,char **argv,int emit){/* control DIR VERB GEN SHA OP NONCE */
    if(argc!=8||(!token(argv[3],15))||!token(argv[4],64)||!hex64(argv[5])||!token(argv[6],96)||!token(argv[7],64))return 64;
    struct sockaddr_un a;memset(&a,0,sizeof a);a.sun_family=AF_UNIX;if(snprintf(a.sun_path,sizeof a.sun_path,"%s/control",argv[2])>=(int)sizeof a.sun_path)return 64;int fd=socket(AF_UNIX,SOCK_SEQPACKET|SOCK_CLOEXEC,0);if(fd<0)return 75;
    int base=checked_directory(argv[2]);struct stat endpoint;if(base<0){close(fd);return 75;}
    if(fstatat(base,"control",&endpoint,AT_SYMLINK_NOFOLLOW)){int why=errno;close(base);close(fd);return why==ENOENT?retired_reply(argv,emit):75;}
    close(base);if(!S_ISSOCK(endpoint.st_mode)||endpoint.st_uid!=geteuid()||endpoint.st_nlink!=1||(endpoint.st_mode&07777)!=0700){close(fd);return 75;}
    uint64_t connect_at=millis();if(connect(fd,(void*)&a,sizeof a)){int why=errno;close(fd);return why==ENOENT||why==ECONNREFUSED?retired_reply(argv,emit):75;}
    struct identity before,after;if(control_peer(fd,argv,&before)){close(fd);return 75;}
    ssize_t bn=read_file("/proc/sys/kernel/random/boot_id",boot,sizeof boot-1);if(bn<1){close(fd);return 75;}boot[bn]=0;if(boot[bn-1]=='\n')boot[bn-1]=0;if(!token(boot,63)){close(fd);return 75;}
    struct timeval t={2,0};if(setsockopt(fd,SOL_SOCKET,SO_RCVTIMEO,&t,sizeof t)){close(fd);return 75;}
    char msg[512];int n=snprintf(msg,sizeof msg,"%s %s %s %s %s",argv[3],argv[4],argv[5],argv[6],argv[7]);if(send(fd,msg,(size_t)n,MSG_NOSIGNAL)!=n){fprintf(stderr,"GENERATION_CONTROL_SEND_FAILED authentication_ms=%llu\n",(unsigned long long)(millis()-connect_at));close(fd);return 74;}
    ssize_t got=recv(fd,snapshot,sizeof snapshot-1,MSG_TRUNC);int valid_peer=!control_peer(fd,argv,&after)&&identity_equal(&before,&after);close(fd);
    if(!valid_peer||got<=0)return retired_reply(argv,emit);if((size_t)got>=sizeof snapshot-1)return 75;snapshot[got]=0;
    if(memchr(snapshot,0,(size_t)got)||control_response(argv,&before,(size_t)got))return 75;
    return !emit||fwrite(snapshot,1,(size_t)got,stdout)==(size_t)got?0:74;
}
static int control(int argc,char **argv){return control_exchange(argc,argv,1);}
#include "broray-updater-migration.h"
#include "broray-updater-bootguard.h"
#include "broray-legacy-control.h"
#include "broray-platform-transaction.h"
#include "broray-recovery-code.h"
#include "broray-service-launcher.h"
#include "broray-platform-launch.h"
#include "broray-updater-service-cycle.h"
int main(int argc,char **argv){
    if(argc>1&&(!strncmp(argv[1],"replacement-service-",20)||!strcmp(argv[1],"replacement-origin-proof")))return replacement_service_entry(argc,argv);
    if(argc>1&&(!strcmp(argv[1],"replacement-start")||!strcmp(argv[1],"replacement-commit")||!strcmp(argv[1],"replacement-commit-check")||!strcmp(argv[1],"replacement-origin-check")))return replacement_start_main(argc,argv);
    if(argc>1&&(!strcmp(argv[1],"replacement-backup")||!strcmp(argv[1],"replacement-install")||!strcmp(argv[1],"replacement-rollback")||!strcmp(argv[1],"replacement-start-intent")))return service_replacement_backup(argc,argv);
    if(argc==3&&!strcmp(argv[1],"generation-boot-verify"))return generation_boot_verify_main(argc,argv);
    if(argc==6){
        if(!strcmp(argv[1],"service-cycle-start"))return service_cycle_main(argc,argv,SC_START);
        if(!strcmp(argv[1],"service-cycle-current"))return service_cycle_main(argc,argv,SC_CURRENT);
        if(!strcmp(argv[1],"service-cycle-stop"))return service_cycle_main(argc,argv,SC_STOP);
        if(!strcmp(argv[1],"service-cycle-restart"))return service_cycle_main(argc,argv,SC_RESTART);
        if((!strcmp(argv[1],"recovery-status")||!strcmp(argv[1],"recovery-commit-check"))&&service_cycle_available(argv[2])!=0)return service_cycle_main(argc,argv,SC_STATUS);
    }
if(argc==2&&!strcmp(argv[1],"--version")){puts("broray-updater-generation/2 supervised-from-birth syscall-containment");return 0;}if(argc==2&&!strcmp(argv[1],"--sha256")){char digest[65];if(hash_fd(STDIN_FILENO,digest))return 74;puts(digest);return 0;}if(argc>1&&!strcmp(argv[1],"recovery-resume"))return recovery_resume_main(argc,argv);if(argc>1&&!strcmp(argv[1],"platform-daemon"))return platform_daemon(argc,argv);if(argc>1&&!strcmp(argv[1],"run"))return run_generation(argc,argv);if(argc>1&&!strcmp(argv[1],"control"))return control(argc,argv);if(argc>1&&(!strcmp(argv[1],"migration-stage")||!strcmp(argv[1],"migration-boundary")||!strcmp(argv[1],"migration-check-staged")))return migration_main(argc,argv);if(argc>1&&!strcmp(argv[1],"guard-bind"))return bootguard_bind_main(argc,argv);if(argc>1&&(!strcmp(argv[1],"guard-stage")||!strcmp(argv[1],"guard-stage-bound")||!strcmp(argv[1],"guard-verify")||!strcmp(argv[1],"guard-verify-bound")||!strcmp(argv[1],"guard-evidence-bound")))return bootguard_main(argc,argv);if(argc>1&&(!strcmp(argv[1],"runtime-retain")||!strcmp(argv[1],"runtime-verify")))return runtime_retain_main(argc,argv);if(argc>1&&(!strcmp(argv[1],"legacy-control-stage")||!strcmp(argv[1],"legacy-control-verify")))return legacy_control_stage_main(argc,argv);if(argc>1&&(!strcmp(argv[1],"recovery-code-stage")||!strcmp(argv[1],"recovery-code-verify")))return recovery_code_main(argc,argv);if(argc>1&&(!strcmp(argv[1],"recovery-service-stop")||!strcmp(argv[1],"recovery-status")||!strcmp(argv[1],"recovery-inspect")||!strcmp(argv[1],"recovery-complete")||!strcmp(argv[1],"recovery-admit")||!strcmp(argv[1],"recovery-retire")||!strcmp(argv[1],"recovery-backup")||!strcmp(argv[1],"recovery-install")||!strcmp(argv[1],"recovery-rollback")||!strcmp(argv[1],"recovery-start-intent")||!strcmp(argv[1],"recovery-start")||!strcmp(argv[1],"recovery-commit")||!strcmp(argv[1],"recovery-stop-current")||!strcmp(argv[1],"recovery-preserve")||!strcmp(argv[1],"recovery-retry")||!strcmp(argv[1],"recovery-commit-check")))return recovery_inspect_main(argc,argv);if(argc>1&&!strcmp(argv[1],"service-host"))return service_host(argc,argv);if(argc>1&&!strcmp(argv[1],"service-status"))return service_host_status(argc,argv);if(argc>1&&!strcmp(argv[1],"service-retired"))return service_host_retired(argc,argv);if(argc>1&&(!strcmp(argv[1],"service-stop-guard")||!strcmp(argv[1],"service-stop-check")))return service_stop_guard(argc,argv);if(argc>1&&!strcmp(argv[1],"service"))return service_client(argc,argv);return 64;}
