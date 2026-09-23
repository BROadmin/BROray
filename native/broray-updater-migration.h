/* Reboot migration preparation only. This module NEVER signals, installs,
 * activates or declares a service STOPPED/ready. Its boot receipt proves that
 * the captured old boot ended; the canonical boot launcher must separately
 * exclude a legacy restart and complete exact installation/readiness.
 * Inputs come from the authenticated slot/coordinator. No /proc PID inventory
 * can substitute for the boot boundary. No caller-provided boot ID is accepted.
 */
#define MIGRATION_FILES 7
static const char *migration_paths[MIGRATION_FILES]={
    "opt/bin/broray-updaterctl","opt/etc/init.d/S22broray-updater",
    "opt/libexec/broray-updater/broray-compat.sh","opt/libexec/broray-updater/broray-migrate-legacy.sh",
    "opt/libexec/broray-updater/minisign","opt/libexec/broray-updater/broray-updater.sh",
    "opt/libexec/broray-updater/xray-wrapper"
};
struct migration_file {char *bytes;size_t size;unsigned mode;int present;char sha[65];};
static int migration_error(const char *reason){fprintf(stderr,"GENERATION_FIRST_ERROR=%s\n",reason);return 75;}
static int migration_path(const char *s){if(s[0]!='/'||strlen(s)>=PATH_MAX)return 0;for(;*s;s++)if((unsigned char)*s<32)return 0;return 1;}
static int migration_directory(const char *path){
    if(!migration_path(path))return -1;
    int fd=open("/",O_RDONLY|O_DIRECTORY|O_CLOEXEC);if(fd<0)return -1;
    char copy[PATH_MAX];strcpy(copy,path);char *save=NULL;
    for(char *p=strtok_r(copy,"/",&save);p;p=strtok_r(NULL,"/",&save)){
        if(!strcmp(p,".")||!strcmp(p,"..")){close(fd);return -1;}
        int next=openat(fd,p,O_RDONLY|O_DIRECTORY|O_NOFOLLOW|O_CLOEXEC);close(fd);if(next<0)return -1;fd=next;
    }
    struct stat st;if(fstat(fd,&st)||st.st_uid!=geteuid()||(st.st_mode&0022)){close(fd);return -1;}return fd;
}
static int migration_relative(int base,const char *relative){
    char copy[PATH_MAX];if(!relative[0]||relative[0]=='/'||strlen(relative)>=sizeof copy)return -1;strcpy(copy,relative);
    /* A simple name is already relative to the held parent. Duplicating that
     * directory via openat(".") only to close it after the final open adds no
     * validation. Keep the exact same no-follow open and error classification;
     * multi-component paths still validate every traversed directory below. */
    if(!strchr(relative,'/')){
        if(!strcmp(relative,".")||!strcmp(relative,".."))return -1;
        int file=openat(base,relative,O_RDONLY|O_NOFOLLOW|O_CLOEXEC|O_NONBLOCK);int why=errno;
        return file<0?(why==ENOENT?-2:-1):file;
    }
    /* Borrow the already held root until the first actual directory opens. */
    int fd=base;
    char *p=copy,*slash;
    while((slash=strchr(p,'/'))){*slash=0;if(!p[0]||!strcmp(p,".")||!strcmp(p,"..")){if(fd!=base)close(fd);return -1;}
        int next=openat(fd,p,O_RDONLY|O_DIRECTORY|O_NOFOLLOW|O_CLOEXEC);int why=errno;if(fd!=base)close(fd);if(next<0)return why==ENOENT?-2:-1;fd=next;p=slash+1;
        struct stat st;if(fstat(fd,&st)||st.st_uid!=geteuid()||(st.st_mode&0022)){close(fd);return -1;}
    }
    if(!p[0]||!strcmp(p,".")||!strcmp(p,"..")){if(fd!=base)close(fd);return -1;}
    int file=openat(fd,p,O_RDONLY|O_NOFOLLOW|O_CLOEXEC|O_NONBLOCK);int why=errno;if(fd!=base)close(fd);return file<0?(why==ENOENT?-2:-1):file;
}
static int migration_read(int base,const char *relative,struct migration_file *r,int absent){
    memset(r,0,sizeof *r);int fd=migration_relative(base,relative);if(fd==-2&&absent){strcpy(r->sha,"-");return 0;}if(fd<0)return -1;
    struct stat st;if(fstat(fd,&st)||!S_ISREG(st.st_mode)||st.st_nlink!=1||st.st_uid!=geteuid()||(st.st_mode&07022)||st.st_size<0||st.st_size>16777216){close(fd);return -1;}
    r->size=(size_t)st.st_size;r->bytes=malloc(r->size+1);if(!r->bytes){close(fd);return -1;}
    size_t at=0;while(at<r->size){ssize_t n=read(fd,r->bytes+at,r->size-at);if(n<0&&errno==EINTR)continue;if(n<=0){close(fd);free(r->bytes);r->bytes=NULL;return -1;}at+=(size_t)n;}
    char tail;struct stat after;int bad=read(fd,&tail,1)!=0||fstat(fd,&after)||after.st_size!=st.st_size||after.st_mode!=st.st_mode||after.st_nlink!=1;close(fd);
    if(bad){free(r->bytes);r->bytes=NULL;return -1;}r->bytes[r->size]=0;r->present=1;r->mode=st.st_mode&0777;digest_bytes(r->bytes,r->size,r->sha);return 0;
}
static int migration_record_existing(int base,const char *name,const char *bytes,size_t n,const struct stat *named){
    if(n>=SNAPSHOT_LIMIT)return -1;
    int fd=openat(base,name,O_RDONLY|O_NOFOLLOW|O_CLOEXEC|O_NONBLOCK);if(fd<0)return -1;
    char *actual=malloc(n+1);int result=-1;if(!actual){close(fd);return -1;}
    /* Keep both exact reads around the same durability barriers. Holding one
     * inode removes repeated opens/allocations and additionally rejects a
     * same-byte replacement at the pathname during either read/fsync. */
    for(int pass=0;pass<2;pass++){
        struct stat held,current;
        if(fstat(fd,&held)||!S_ISREG(held.st_mode)||held.st_uid!=geteuid()||held.st_nlink!=1||
           (held.st_mode&07777)!=0600||held.st_size!=(off_t)n||held.st_dev!=named->st_dev||held.st_ino!=named->st_ino||
           fstatat(base,name,&current,AT_SYMLINK_NOFOLLOW)||current.st_dev!=held.st_dev||current.st_ino!=held.st_ino||
           current.st_mode!=held.st_mode||current.st_uid!=held.st_uid||current.st_nlink!=1||current.st_size!=held.st_size||
           (pass&&lseek(fd,0,SEEK_SET)!=0))goto done;
        size_t at=0;while(at<n){ssize_t got=read(fd,actual+at,n-at);if(got<0&&errno==EINTR)continue;if(got<=0)goto done;at+=(size_t)got;}
        char extra;if(read(fd,&extra,1)!=0||memcmp(actual,bytes,n)||
           fstatat(base,name,&current,AT_SYMLINK_NOFOLLOW)||current.st_dev!=held.st_dev||current.st_ino!=held.st_ino||
           current.st_mode!=held.st_mode||current.st_uid!=held.st_uid||current.st_nlink!=1||current.st_size!=held.st_size)goto done;
        if(!pass&&(fsync(fd)||fsync(base)))goto done;
    }
    result=0;
done:free(actual);if(close(fd))result=-1;return result;
}
static int migration_record(int base,const char *name,const char *bytes,size_t n,int allow_create){
    struct stat st;
    if(!fstatat(base,name,&st,AT_SYMLINK_NOFOLLOW)){
        /* An earlier attempt may have died after linking, before directory
         * fsync. Matching bytes alone do not acknowledge durable publication. */
        return migration_record_existing(base,name,bytes,n,&st);
    }
    if(errno!=ENOENT||!allow_create)return -1;
    char pending[128];if(snprintf(pending,sizeof pending,"%s.pending",name)>=(int)sizeof pending)return -1;
    int fd=openat(base,pending,O_WRONLY|O_CREAT|O_EXCL|O_NOFOLLOW|O_CLOEXEC,0600);if(fd<0)return -1;
    if(fstat(fd,&st)||!S_ISREG(st.st_mode)||st.st_nlink!=1||st.st_uid!=geteuid()||(st.st_mode&07777)!=0600){close(fd);return -1;}
    size_t at=0;while(at<n){ssize_t got=write(fd,bytes+at,n-at);if(got<0&&errno==EINTR)continue;if(got<=0){close(fd);return -1;}at+=(size_t)got;}
    int rc=fsync(fd);if(close(fd))rc=-1;
    if(rc||linkat(base,pending,base,name,0)||unlinkat(base,pending,0)||fsync(base))return -1;
    return migration_record(base,name,bytes,n,0);
}
static int migration_names(int base,int has_intent){
    DIR *d=directory_stream(base);if(!d)return -1;struct dirent *entry;int rc=0;errno=0;
    while((entry=readdir(d))){const char *n=entry->d_name;if(!strcmp(n,".")||!strcmp(n,".."))continue;
        if(!has_intent){rc=-1;break;}
        if(!strcmp(n,"intent.record")||!strcmp(n,"manifest.record")||!strcmp(n,"staged.receipt")||!strcmp(n,"boot.receipt"))continue;
        int allowed=0;for(int i=0;i<MIGRATION_FILES;i++){char expected[32];snprintf(expected,sizeof expected,"file-%d",i);if(!strcmp(n,expected))allowed=1;}
        if(!allowed){rc=-1;break;}errno=0;
    }
    if(!entry&&errno)rc=-1;closedir(d);return rc;
}
static int migration_boot(char out[64]){ssize_t n=read_file("/proc/sys/kernel/random/boot_id",out,63);if(n<=0)return -1;out[n]=0;if(out[n-1]=='\n')out[n-1]=0;return strlen(out)==36&&token(out,63)?0:-1;}
static int migration_sync_directory(const char *path,int base){
    char parent[PATH_MAX];if(strlen(path)>=sizeof parent)return -1;strcpy(parent,path);char *slash=strrchr(parent,'/');
    if(!slash||slash==parent||!slash[1])return -1;char name[NAME_MAX+1];if(strlen(slash+1)>NAME_MAX)return -1;strcpy(name,slash+1);*slash=0;
    /* The coordinator already owns a durable private parent. Confirm this
     * newly created child entry points to the same directory held by flock. */
    int fd=checked_directory(parent);struct stat held,named;if(fd<0)return -1;
    int bad=fstat(base,&held)||fstatat(fd,name,&named,AT_SYMLINK_NOFOLLOW)||!S_ISDIR(named.st_mode)||held.st_dev!=named.st_dev||held.st_ino!=named.st_ino||fsync(base)||fsync(fd);close(fd);return bad?-1:0;
}
static int migration_main(int argc,char **argv){
    /* migration-stage|migration-boundary DIR LIVE_ROOT PAYLOAD SHA OP NONCE running|stopped */
    if(argc!=9||!migration_path(argv[2])||!migration_path(argv[3])||!migration_path(argv[4])||!hex64(argv[5])||!token(argv[6],96)||!token(argv[7],64)||(strcmp(argv[8],"running")&&strcmp(argv[8],"stopped")))return 64;
    int boundary=!strcmp(argv[1],"migration-boundary"),result=75,base=-1,live=-1,source=-1;
    char current_boot[64],old_boot[64],*intent=NULL,*prior=NULL,*prefix=NULL,*manifest_text=NULL;size_t intent_size=0,prior_size=0,prefix_size=0,manifest_size=0;
    struct migration_file target[MIGRATION_FILES],before[MIGRATION_FILES],manifest_file;memset(target,0,sizeof target);memset(before,0,sizeof before);memset(&manifest_file,0,sizeof manifest_file);
    const char *error="MIGRATION_INPUT_UNCONFIRMED";umask(077);
    base=checked_directory(argv[2]);live=migration_directory(argv[3]);source=migration_directory(argv[4]);
    if(base<0||live<0||source<0||flock(base,LOCK_EX|LOCK_NB)||migration_boot(current_boot)||migration_sync_directory(argv[2],base))goto done;
    struct stat st;int has_intent=!fstatat(base,"intent.record",&st,AT_SYMLINK_NOFOLLOW);if(!has_intent&&errno!=ENOENT)goto done;
    error="MIGRATION_EVIDENCE_UNCONFIRMED";if(migration_names(base,has_intent))goto done;
    if(migration_read(source,"SHA256SUMS",&manifest_file,0)||strcmp(manifest_file.sha,argv[5]))goto done;
    FILE *mf=open_memstream(&manifest_text,&manifest_size);if(!mf)goto done;int bad=0;
    for(int i=0;i<MIGRATION_FILES;i++){
        if(migration_read(source,migration_paths[i],&target[i],0)||target[i].mode!=0755||migration_read(live,migration_paths[i],&before[i],1)){bad=1;break;}
        fprintf(mf,"%s  %s\n",target[i].sha,migration_paths[i]);
    }
    if(fclose(mf))bad=1;
    /* Require each fixed allowlisted path exactly once; canonical source
     * manifests may order their lines differently. No extra path or token. */
    unsigned seen=0;size_t offset=0;
    while(!bad&&offset<manifest_file.size){char *line=manifest_file.bytes+offset,*nl=memchr(line,'\n',manifest_file.size-offset);if(!nl){bad=1;break;}
        int matched=0;for(int i=0;i<MIGRATION_FILES;i++){char row[256];int n=snprintf(row,sizeof row,"%s  %s",target[i].sha,migration_paths[i]);if(n==(int)(nl-line)&&!memcmp(line,row,(size_t)n)&&!(seen&(1U<<i))){seen|=1U<<i;matched=1;break;}}
        if(!matched)bad=1;offset=(size_t)(nl-manifest_file.bytes)+1;
    }
    if(bad||seen!=((1U<<MIGRATION_FILES)-1))goto done;
    FILE *pf=open_memstream(&prefix,&prefix_size);if(!pf)goto done;
    fprintf(pf,"BROray-updater-migration/1\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n",argv[2],argv[3],argv[4],argv[5],argv[6],argv[7],argv[8]);if(fclose(pf))goto done;
    strcpy(old_boot,current_boot);
    if(has_intent){
        if(safe_bytes_at(base,"intent.record",&prior,&prior_size)||prior_size<prefix_size+37||memcmp(prior,prefix,prefix_size)||prior[prefix_size+36]!='\n')goto done;
        memcpy(old_boot,prior+prefix_size,36);old_boot[36]=0;if(!token(old_boot,36))goto done;
    }else if(boundary)goto done;
    FILE *record=open_memstream(&intent,&intent_size);if(!record)goto done;fwrite(prefix,1,prefix_size,record);fprintf(record,"%s\n",old_boot);
    for(int i=0;i<MIGRATION_FILES;i++)fprintf(record,"%s\t%d\t%04o\t%s\t%04o\t%s\n",migration_paths[i],before[i].present,before[i].mode,before[i].sha,target[i].mode,target[i].sha);
    if(fclose(record))goto done;
    if(has_intent&&(prior_size!=intent_size||memcmp(prior,intent,intent_size)))goto done;
    int staged=!fstatat(base,"staged.receipt",&st,AT_SYMLINK_NOFOLLOW);if(!staged&&errno!=ENOENT)goto done;
    if(boundary&&!staged)goto done;
    if(!boundary&&strcmp(old_boot,current_boot)){error="MIGRATION_BOOT_BOUNDARY_REQUIRES_VERIFICATION";goto done;}
    /* No mutation of live files. Intent is durable before ANY staged copy. */
    if(migration_record(base,"intent.record",intent,intent_size,!boundary&&!has_intent))goto done;
    if(migration_record(base,"manifest.record",manifest_file.bytes,manifest_file.size,!boundary&&!staged))goto done;
    for(int i=0;i<MIGRATION_FILES;i++){char name[32];snprintf(name,sizeof name,"file-%d",i);if(migration_record(base,name,target[i].bytes,target[i].size,!boundary&&!staged))goto done;}
    /* Current live inventory must still equal the captured before-image. */
    for(int i=0;i<MIGRATION_FILES;i++){struct migration_file again;
        if(migration_read(live,migration_paths[i],&again,1))goto done;
        int changed=again.present!=before[i].present||again.mode!=before[i].mode||strcmp(again.sha,before[i].sha);free(again.bytes);if(changed){error="MIGRATION_PLATFORM_CHANGED";goto done;}
    }
    char digest[65],receipt[128];digest_bytes(intent,intent_size,digest);int n=snprintf(receipt,sizeof receipt,"BROray-migration-staged/1\n%s\n",digest);
    if(migration_record(base,"intent.record",intent,intent_size,0)||migration_record(base,"staged.receipt",receipt,(size_t)n,!boundary&&!staged))goto done;
    if(boundary){
        if(!strcmp(old_boot,current_boot)){error="MIGRATION_REBOOT_REQUIRED";goto done;}
        char proof[256];n=snprintf(proof,sizeof proof,"BROray-migration-boot/1\n%s\n%s\n%s\n",digest,old_boot,current_boot);
        if(migration_record(base,"boot.receipt",proof,(size_t)n,1))goto done;
    }
    printf("{\"ok\":true,\"phase\":\"%s\",\"activationAllowed\":false,\"serviceStopped\":false,\"oldBootId\":\"%s\",\"currentBootId\":\"%s\",\"intentSha256\":\"%s\"}\n",boundary?"BOOT_BOUNDARY_PROVEN":"REBOOT_REQUIRED",old_boot,current_boot,digest);result=0;
done:
    for(int i=0;i<MIGRATION_FILES;i++){free(target[i].bytes);free(before[i].bytes);}free(manifest_file.bytes);free(manifest_text);free(prefix);free(prior);free(intent);
    if(source>=0)close(source);if(live>=0)close(live);if(base>=0)close(base);return result?migration_error(error):0;
}
