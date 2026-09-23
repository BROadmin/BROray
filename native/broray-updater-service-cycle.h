/* Ordinary S22 lifecycle, deliberately separate from migration retries.
 * The sealed, completed migration is read-only provenance. Every later launch
 * has its own durable intent under updater/cycles and uses the existing native
 * platform-daemon, lifetime exclusion and authenticated from-birth supervisor.
 */
#define SC_LIMIT 128
struct sc_node {char id[65],born[64],launch[65],transaction[65],host[65],chain[65];unsigned index;};
struct sc_context {
    int state,guard,op,up,cycles,transition,may_publish;
    char root[PATH_MAX],statepath[PATH_MAX],oppath[PATH_MAX],uppath[PATH_MAX],cyclepath[PATH_MAX];
    char origin[97],migration[65],nonce[65],native[65],tree[65],seal[65],initial[65];
    struct bg_input input;struct sc_node nodes[SC_LIMIT];unsigned count,baseline,current;
};
static struct sc_context sc;
static int sc_stopped_seal(struct sc_node *n,int publish);
struct gb_record;
static int sc_boot_proof(struct sc_node *n,struct gb_record *b,char sha[65]);
static int sc_boot_markers(struct sc_node *n,int finish);
static int sc_predecessor(struct sc_node *n,char retired[65],char host[65],char ready[65],char tree[65]);
static int sc_fail(const char *reason){fprintf(stderr,"SERVICE_CYCLE_FIRST_ERROR=%s\n",reason);return 75;}
static int sc_join(char out[PATH_MAX],const char *a,const char *b){
    int n=snprintf(out,PATH_MAX,"%s%s%s",a,!strcmp(a,"/")?"":"/",b);return n<0||n>=PATH_MAX?-1:0;
}
static int sc_file(int base,const char *name,struct migration_file *f){
    if(migration_read(base,name,f,0))return -1;
    return f->mode!=0600||f->size>=SNAPSHOT_LIMIT||memchr(f->bytes,0,f->size)?-1:0;
}
/* Read either a completed service record or its exact interrupted publication.
 * The only allowed extra link is the matching .pending name to the SAME inode.
 * Reading grants no authority; callers must derive and compare canonical bytes
 * from the authenticated predecessor before sc_publish may finish the link. */
struct sc_record_pair {struct stat identity;int names;};
static int sc_record_names(int base,const char *name,char pending[128],struct sc_record_pair *pair){
    if(strchr(name,'/')||snprintf(pending,128,"%s.pending",name)>=128)return -1;
    struct stat a,b;int one=fstatat(base,name,&a,AT_SYMLINK_NOFOLLOW)==0;
    if(!one&&errno!=ENOENT)return -1;
    int two=fstatat(base,pending,&b,AT_SYMLINK_NOFOLLOW)==0;
    if(!two&&errno!=ENOENT)return -1;
    pair->names=one+2*two;if(!pair->names)return 0;
    if(one&&two&&(a.st_dev!=b.st_dev||a.st_ino!=b.st_ino))return -1;
    pair->identity=one?a:b;struct stat *s=&pair->identity;
    return !S_ISREG(s->st_mode)||s->st_uid!=geteuid()||(s->st_mode&07777)!=0600||
           s->st_nlink!=(nlink_t)(one&&two?2:1)||s->st_size<0||s->st_size>=SNAPSHOT_LIMIT?-1:0;
}
static int sc_same_record(const struct stat *a,const struct stat *b){
    return a->st_dev==b->st_dev&&a->st_ino==b->st_ino&&a->st_size==b->st_size&&
           a->st_mode==b->st_mode&&a->st_uid==b->st_uid;
}
static int sc_service_file(int base,const char *name,struct migration_file *f,struct sc_record_pair *proof){
    char pending[128];struct sc_record_pair before,after;memset(f,0,sizeof *f);
    if(sc_record_names(base,name,pending,&before)||!before.names)return -1;
    int fd=openat(base,before.names&1?name:pending,O_RDONLY|O_NOFOLLOW|O_CLOEXEC|O_NONBLOCK);
    struct stat held;if(fd<0)return -1;
    if(fstat(fd,&held)||!sc_same_record(&held,&before.identity)||held.st_nlink!=before.identity.st_nlink){close(fd);return -1;}
    f->size=(size_t)held.st_size;f->bytes=malloc(f->size+1);if(!f->bytes){close(fd);return -1;}
    size_t used=0;int bad=0;
    while(used<f->size){ssize_t n=read(fd,f->bytes+used,f->size-used);if(n<0&&errno==EINTR)continue;if(n<=0){bad=1;break;}used+=(size_t)n;}
    char tail;
    if(!bad)bad=read(fd,&tail,1)!=0||memchr(f->bytes,0,f->size)||
        sc_record_names(base,name,pending,&after)||before.names!=after.names||!sc_same_record(&before.identity,&after.identity);
    close(fd);if(bad){free(f->bytes);f->bytes=NULL;return -1;}
    f->bytes[f->size]=0;f->mode=0600;f->present=1;digest_bytes(f->bytes,f->size,f->sha);
    if(proof)*proof=before;return 0;
}
static int sc_record_exists(int base,const char *name){
    char pending[128];struct sc_record_pair pair;return sc_record_names(base,name,pending,&pair)?-1:pair.names!=0;
}
static int sc_publish(int base,const char *name,const char *bytes,size_t size,int create){
    char pending[128];struct sc_record_pair pair,after;struct migration_file saved;memset(&saved,0,sizeof saved);
    if(size>=SNAPSHOT_LIMIT||sc_record_names(base,name,pending,&pair))return -1;
    if(!pair.names)return sc.may_publish&&create?migration_record(base,name,bytes,size,1):-1;
    if(!sc.may_publish&&pair.names!=1)return -1;
    if(sc_service_file(base,name,&saved,&pair))return -1;
    int bad=saved.size!=size||memcmp(saved.bytes,bytes,size);free(saved.bytes);if(bad)return -1;
    if(pair.names==1)return migration_record_existing(base,name,bytes,size,&pair.identity);
    int fd=openat(base,pair.names&1?name:pending,O_RDONLY|O_NOFOLLOW|O_CLOEXEC|O_NONBLOCK),result=-1;struct stat held;
    if(fd<0)return -1;
    if(fstat(fd,&held)||!sc_same_record(&held,&pair.identity)||held.st_nlink!=pair.identity.st_nlink||fsync(fd)||fsync(base))goto done;
    if(pair.names==2&&linkat(base,pending,base,name,0))goto done;
    if(sc_service_file(base,name,&saved,&after))goto done;
    bad=after.names!=3||!sc_same_record(&after.identity,&pair.identity)||saved.size!=size||memcmp(saved.bytes,bytes,size);
    free(saved.bytes);saved.bytes=NULL;if(bad||fsync(fd)||fsync(base))goto done;
    /* Only the independently validated publication alias is removed. The
     * published record is never replaced, even when an interrupted retry
     * presents corrupt bytes, another inode, an extra hardlink or a symlink. */
    if(sc_record_names(base,name,pending,&after)||after.names!=3||!sc_same_record(&after.identity,&pair.identity)||
       unlinkat(base,pending,0)||fsync(fd)||fsync(base)||sc_record_names(base,name,pending,&after)||
       after.names!=1||!sc_same_record(&after.identity,&pair.identity)||migration_record_existing(base,name,bytes,size,&after.identity))goto done;
    result=0;
 done:close(fd);return result;
}
static int sc_field(const char *text,const char *name,char *out,size_t capacity){
    char key[128];int n=snprintf(key,sizeof key,"\"%s\":\"",name);if(n<0||n>=(int)sizeof key)return -1;
    const char *p=strstr(text,key);if(!p)return -1;p+=n;const char *end=strchr(p,'"');
    if(!end||end==p||(size_t)(end-p)>=capacity||memchr(p,'\\',(size_t)(end-p)))return -1;
    memcpy(out,p,(size_t)(end-p));out[end-p]=0;return 0;
}
/* A boot-ended generation is not forged STOPPED. Its complete immutable
 * ledger and the independently pinned READY revision remain untouched. The
 * receipt proves only that its real kernel boot has ended; no PID is signalled.
 */
struct gb_record {
    char id[65],manifest[65],from[64],through[64],scope[65],seal[65],ready[65];
    char host[65],journal[65],launch[65],transaction[65],inventory[65],last[65];
    unsigned long total;
};
static int gb_text(const struct gb_record *b,char out[2048]){
    return snprintf(out,2048,"BROray-generation-boot-ended/1\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%lu\n%s\n%s\n",b->id,b->manifest,b->from,b->through,b->scope,b->seal,b->ready,b->host,b->journal,b->launch,b->transaction,b->total,b->inventory,b->last);
}
static int gb_parent(const char *domain,const char *id,char up[PATH_MAX]){
    if(strlen(domain)>=PATH_MAX)return -1;strcpy(up,domain);char *p=strrchr(up,'/');
    if(!p||strcmp(p+1,id))return -1;*p=0;p=strrchr(up,'/');
    if(!p||strcmp(p+1,"generations"))return -1;*p=0;return 0;
}
static int gb_measure(int base,const char *domain,struct gb_record *b){
    char current[64],up[PATH_MAX],path[PATH_MAX],hostpath[PATH_MAX],name[128],prefix[512],sha[65],prior[65]="";
    struct migration_file ready,origin,host;memset(&ready,0,sizeof ready);memset(&origin,0,sizeof origin);memset(&host,0,sizeof host);
    int cycles=-1,h=-1,witness=-1,result=-1;DIR *dir=NULL;unsigned long maximum=0,found=0,anchor=0;
    if(migration_boot(current)||!token(b->id,64)||!hex64(b->manifest)||strlen(b->from)!=36||strlen(b->through)!=36||
       !token(b->from,36)||!token(b->through,36)||!strcmp(b->from,current)||!strcmp(b->from,b->through)||
       gb_parent(domain,b->id,up)||sc_join(path,up,"cycles"))goto done;
    scope_digest(domain,sha);if(strcmp(sha,b->scope))goto done;
    cycles=checked_directory(path);if(cycles<0||sc_file(cycles,"origin.record",&origin)||strcmp(origin.sha,b->seal))goto done;
    int nn=snprintf(name,sizeof name,"ready-%s.record",b->id);
    int pn=snprintf(prefix,sizeof prefix,"BROray-service-ready/1\n%s\n%s\n%s\n%s\n",b->seal,b->launch,b->transaction,b->host);
    if(nn<0||nn>=(int)sizeof name||pn<0||pn>=(int)sizeof prefix||sc_file(cycles,name,&ready)||
       strcmp(ready.sha,b->ready)||ready.size<=(size_t)pn||memcmp(ready.bytes,prefix,(size_t)pn))goto done;
    const char *accepted=ready.bytes+pn,*rev=strstr(accepted,",\"revision\":");char field[256],observed[65];
    if(!rev||sscanf(rev,",\"revision\":%lu,",&anchor)!=1||!anchor||anchor>1000000||
       sc_field(accepted,"generationId",observed,sizeof observed)||strcmp(observed,b->id)||
       sc_field(accepted,"bootId",observed,sizeof observed)||strcmp(observed,b->from)||
       !strstr(accepted,"\"state\":\"RUNNING\",\"supervisedFromBirth\":true,")||!strstr(accepted,"\"platformReady\":true,"))goto done;
    if(peer_executable_hash(getpid(),sha))goto done;
    snprintf(field,sizeof field,"\"nativeSha256\":\"%s\"",sha);if(!strstr(accepted,field))goto done;
    if(sc_join(path,up,"hosts")||sc_join(hostpath,path,b->id))goto done;
    h=checked_directory(hostpath);if(h<0||sc_file(h,"host.record",&host)||strcmp(host.sha,b->host)||service_journal_digest(h,b->journal))goto done;
    if(sc_field(host.bytes,"bootId",observed,sizeof observed)||strcmp(observed,b->from))goto done;
    dir=directory_stream(base);if(!dir)goto done;struct dirent *e;int bad=0;errno=0;
    while((e=readdir(dir))){const char *s=e->d_name;
        if(!strcmp(s,".")||!strcmp(s,".."))continue;
        if(!strcmp(s,"control")||!strcmp(s,"lifetime.lock")){
            struct stat st;if(fstatat(base,s,&st,AT_SYMLINK_NOFOLLOW)||st.st_uid!=geteuid()||st.st_nlink!=1){bad=1;break;}
            if(!strcmp(s,"control")){if(!S_ISSOCK(st.st_mode)||(st.st_mode&07777)!=0700){bad=1;break;}}
            else if(!S_ISREG(st.st_mode)||(st.st_mode&07777)!=0600||st.st_size){bad=1;break;}
            errno=0;continue;
        }
        if(!strcmp(s,"boot-ended.receipt")||!strcmp(s,"boot-ended.receipt.pending")){
            char pending[128];struct sc_record_pair pair;if(sc_record_names(base,"boot-ended.receipt",pending,&pair)||!pair.names){bad=1;break;}
            errno=0;continue;
        }
        unsigned long nr=1;char extra,want[64];
        if(strcmp(s,"state.json")){
            if(sscanf(s,"revision-%20lu.json%c",&nr,&extra)!=1||nr<2||nr>1000000){bad=1;break;}
            record_name(nr,want);if(strcmp(want,s)){bad=1;break;}
        }
        found++;if(nr>maximum)maximum=nr;errno=0;
    }
    if(!e&&errno)bad=1;closedir(dir);dir=NULL;if(bad||found!=maximum||maximum<anchor)goto done;
    if(sc_join(path,up,"starts")||sc_join(hostpath,path,b->id)||sc_join(path,hostpath,"ledger-witnesses"))goto done;
    witness=checked_directory(path);if(witness<0)goto done;
    char witness_inventory[65];if(ledger_inventory(witness,maximum,witness_inventory))goto done;
    struct gen_sha inventory;gen_sha_init(&inventory);
    for(unsigned long nr=1;nr<=maximum;nr++){
        char *bytes=NULL;size_t size=0;record_name(nr,name);
        if(safe_bytes_at(base,name,&bytes,&size))goto done;
        int k=snprintf(prefix,sizeof prefix,"{\"schemaVersion\":2,\"contract\":\"broray-updater-generation/2\",\"generationId\":\"%s\",\"platformManifestSha256\":\"%s\",\"bootId\":\"%s\",\"revision\":%lu,\"previousRecordSha256\":\"%s\",",b->id,b->manifest,b->from,nr,prior);
        bad=k<0||k>=(int)sizeof prefix||size<(size_t)k+2||memchr(bytes,0,size)||memcmp(bytes,prefix,(size_t)k)||memcmp(bytes+size-2,"}\n",2)||!strstr(bytes,"\"supervisedFromBirth\":true,");
        if(nr==anchor&&(size!=ready.size-(size_t)pn||memcmp(bytes,accepted,size)))bad=1;
        if(!bad){
            digest_bytes(bytes,size,prior);char expected[384];
            int w=snprintf(expected,sizeof expected,"BROray-platform-ledger-witness/1\n%s\n%s\n%s\n%lu\n%s\n",b->id,b->manifest,b->from,nr,prior);
            if(w<0||w>=(int)sizeof expected||bg_record_exact(witness,name,expected,(size_t)w))bad=1;
            gen_sha_add(&inventory,name,strlen(name)+1);gen_sha_add(&inventory,prior,64);
        }
        free(bytes);if(bad)goto done;
    }
    b->total=maximum;strcpy(b->last,prior);gen_sha_end(&inventory,b->inventory);result=0;
 done:if(witness>=0)close(witness);if(dir)closedir(dir);if(cycles>=0)close(cycles);if(h>=0)close(h);free(ready.bytes);free(origin.bytes);free(host.bytes);return result;
}
static int gb_validate(int base,const char *domain,struct gb_record *out){
    struct migration_file record;memset(&record,0,sizeof record);struct gb_record b;memset(&b,0,sizeof b);
    char canonical[2048],extra;int result=-1;
    if(sc_file(base,"boot-ended.receipt",&record))goto done;
    int count=sscanf(record.bytes,"BROray-generation-boot-ended/1\n%64s\n%64s\n%63s\n%63s\n%64s\n%64s\n%64s\n%64s\n%64s\n%64s\n%64s\n%lu\n%64s\n%64s\n%c",b.id,b.manifest,b.from,b.through,b.scope,b.seal,b.ready,b.host,b.journal,b.launch,b.transaction,&b.total,b.inventory,b.last,&extra);
    int n=gb_text(&b,canonical);if(count!=14||n<0||n>=2048||record.size!=(size_t)n||memcmp(record.bytes,canonical,(size_t)n)||
       !hex64(b.scope)||!hex64(b.seal)||!hex64(b.ready)||!hex64(b.host)||!hex64(b.journal)||!hex64(b.launch)||!hex64(b.transaction)||!hex64(b.inventory)||!hex64(b.last))goto done;
    struct gb_record actual=b;if(gb_measure(base,domain,&actual))goto done;
    n=gb_text(&actual,canonical);if(n<0||n>=2048||record.size!=(size_t)n||memcmp(record.bytes,canonical,(size_t)n))goto done;
    if(out)*out=b;result=0;
 done:free(record.bytes);return result;
}
static int generation_boot_retirement_valid(int base,const char *domain,const char *manifest_sha,const char *current){
    struct gb_record b;return gb_validate(base,domain,&b)||strcmp(b.manifest,manifest_sha)||(current&&!strcmp(current,b.id))?-1:0;
}
static int generation_boot_verify_main(int argc,char **argv){
    if(argc!=3)return 64;int fd=checked_directory(argv[2]);struct gb_record b;
    if(fd<0||gb_validate(fd,argv[2],&b)){if(fd>=0)close(fd);return 75;}close(fd);
    printf("{\"ok\":true,\"phase\":\"BOOT_ENDED_HISTORY_VERIFIED\",\"generationId\":\"%s\",\"oldBootId\":\"%s\",\"serviceStopped\":false,\"signalsAuthorized\":false}\n",b.id,b.from);return 0;
}

static int sc_namecmp(const void *a,const void *b){return strcmp(a,b);}
/* Canonical origin inventory includes names, modes, exact file bytes and the
 * one retired-fence symlink. It follows no symlink and records no live PID. */
static int sc_tree_walk(int fd,const char *relative,struct gen_sha *hash,unsigned *total,unsigned depth,const char *tree_root){
    if(depth>20)return -1;DIR *d=directory_stream(fd);if(!d)return -1;
    char (*names)[NAME_MAX+1]=calloc(4096,sizeof *names);if(!names){closedir(d);return -1;}
    unsigned count=0;struct dirent *e;int bad=0;errno=0;
    while((e=readdir(d))){if(!strcmp(e->d_name,".")||!strcmp(e->d_name,".."))continue;
        if(count==4096||++*total>4096){bad=1;break;}strcpy(names[count++],e->d_name);errno=0;}
    if(!e&&errno)bad=1;closedir(d);if(bad){free(names);return -1;}
    qsort(names,count,sizeof *names,sc_namecmp);
    for(unsigned i=0;i<count;i++){
        struct stat st,after;char rel[PATH_MAX],meta[96],sha[65];
        int n=snprintf(rel,sizeof rel,"%s%s%s",relative,*relative?"/":"",names[i]);
        if(n<0||n>=(int)sizeof rel||fstatat(fd,names[i],&st,AT_SYMLINK_NOFOLLOW)||st.st_uid!=geteuid()){bad=1;break;}
        n=snprintf(meta,sizeof meta,"%o\n",(unsigned)(st.st_mode&0177777));if(n<0||n>=(int)sizeof meta){bad=1;break;}
        gen_sha_add(hash,rel,strlen(rel)+1);gen_sha_add(hash,meta,(size_t)n);
        if(S_ISDIR(st.st_mode)){
            int child=openat(fd,names[i],O_RDONLY|O_DIRECTORY|O_NOFOLLOW|O_CLOEXEC);
            if(child<0||(st.st_mode&0022)||sc_tree_walk(child,rel,hash,total,depth+1,tree_root))bad=1;
            if(child>=0)close(child);
        }else if(S_ISREG(st.st_mode)){
            struct migration_file f;memset(&f,0,sizeof f);
            if(migration_read(fd,names[i],&f,0))bad=1;
            else{strcpy(sha,f.sha);gen_sha_add(hash,sha,64);}free(f.bytes);
        }else if(S_ISLNK(st.st_mode)&&!*relative&&!strcmp(names[i],"retired-lock")){
            char target[PATH_MAX],expected[PATH_MAX];ssize_t got=readlinkat(fd,names[i],target,sizeof target-1);
            if(got<=0||got>=(ssize_t)sizeof target-1||sc_join(expected,tree_root,"fence"))bad=1;
            else{target[got]=0;if(strcmp(target,expected))bad=1;else gen_sha_add(hash,target,(size_t)got+1);}
        }else bad=1;
        if(bad||fstatat(fd,names[i],&after,AT_SYMLINK_NOFOLLOW)||after.st_dev!=st.st_dev||after.st_ino!=st.st_ino||after.st_mode!=st.st_mode||after.st_size!=st.st_size||after.st_nlink!=st.st_nlink){bad=1;break;}
    }
    free(names);return bad?-1:0;
}
static int sc_tree_hash_at(int fd,const char *root,char sha[65]){struct gen_sha h;unsigned count=0;gen_sha_init(&h);if(sc_tree_walk(fd,"",&h,&count,0,root))return -1;gen_sha_end(&h,sha);return 0;}
static int sc_tree_hash(int fd,char sha[65]){return sc_tree_hash_at(fd,sc.oppath,sha);}
static int sc_lock_exact(int parent,const char *name,int create){
    int fd=openat(parent,name,O_RDWR|O_NOFOLLOW|O_CLOEXEC|(create?O_CREAT:0),0600);struct stat held,named;
    if(fd<0)return -1;
    if(fstat(fd,&held)||!S_ISREG(held.st_mode)||held.st_uid!=geteuid()||held.st_nlink!=1||(held.st_mode&07777)!=0600||held.st_size||
       fstatat(parent,name,&named,AT_SYMLINK_NOFOLLOW)||held.st_dev!=named.st_dev||held.st_ino!=named.st_ino||held.st_mode!=named.st_mode||named.st_nlink!=1||named.st_uid!=held.st_uid||named.st_size||
       flock(fd,LOCK_EX|LOCK_NB)||(create&&(fsync(fd)||fsync(parent)))){close(fd);return -1;}return fd;
}
static int sc_call(char **argv,const char *verb,char *reply,size_t capacity){
    int p[2];if(pipe2(p,O_CLOEXEC))return 75;pid_t child=fork();if(child<0){close(p[0]);close(p[1]);return 75;}
    if(!child){close(p[0]);if(dup2(p[1],1)<0)_exit(74);close(p[1]);
        int flags=fcntl(sc.guard,F_GETFD);if(flags<0||fcntl(sc.guard,F_SETFD,flags&~FD_CLOEXEC))_exit(74);
        if(sc.transition>=0){flags=fcntl(sc.transition,F_GETFD);if(flags<0||fcntl(sc.transition,F_SETFD,flags&~FD_CLOEXEC))_exit(74);}
        char *args[]={argv[0],(char*)verb,argv[2],argv[3],argv[4],argv[5],NULL};
        if(!strcmp(verb,"recovery-commit-check")){
            /* This is ONLY the initial origin's read-only commit proof. The
             * public dispatcher sees cycles/ and routes STATUS back here;
             * re-entering it while the origin is partial would recurse into
             * the very seal being established. Keep the complete original
             * proof, guard and live identities, bypassing only that dispatch. */
            if(sc.transition>=0)_exit(75);
            int code=recovery_inspect_main(6,args);if(fflush(stdout))code=75;_exit(code);
        }
        execv("/proc/self/exe",args);_exit(74);}
    close(p[1]);size_t used=0;int bad=0;
    for(;;){char b[4096];ssize_t n=read(p[0],b,sizeof b);if(n<0&&errno==EINTR)continue;if(n<0){bad=1;break;}if(!n)break;
        if(used+(size_t)n>=capacity){bad=1;continue;}memcpy(reply+used,b,(size_t)n);used+=(size_t)n;}
    close(p[0]);reply[used]=0;int status;pid_t got;do{got=waitpid(child,&status,0);}while(got<0&&errno==EINTR);
    return bad||got!=child||!WIFEXITED(status)?75:WEXITSTATUS(status);
}
static int sc_load_node(struct sc_node *n){
    char start[PATH_MAX],parent[PATH_MAX];if(sc_join(parent,sc.uppath,"starts")||sc_join(start,parent,n->id)||pl_load(start,n->launch,n->transaction,0))return -1;
    return strcmp(pl.root,sc.root)||strcmp(pl.id,n->id)||strcmp(pl.op,sc.origin)||strcmp(pl.nonce,sc.nonce)||strcmp(pl.native,sc.native)||strcmp(pl.manifest,sc.input.manifest)?-1:0;
}
static int sc_host_record(struct sc_node *n){
    if(sc_load_node(n))return -1;int host=checked_directory(pl.host);struct migration_file f;memset(&f,0,sizeof f);int bad=host<0;
    if(!bad)bad=sc_file(host,"host.record",&f)||strcmp(f.sha,n->host);
    free(f.bytes);if(host>=0)close(host);return bad?-1:0;
}
static char *sc_line(char **cursor){char *p=*cursor,*end=strchr(p,'\n');if(!end)return NULL;*end=0;*cursor=end+1;return p;}
static int sc_seal_text(char **text,size_t *size){
    FILE *f=open_memstream(text,size);if(!f)return -1;
    fprintf(f,"BROray-service-origin/1\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%u\n",sc.root,sc.origin,sc.migration,sc.nonce,sc.native,sc.input.manifest,sc.tree,sc.initial,sc.baseline);
    for(unsigned i=0;i<sc.baseline;i++){struct sc_node *n=&sc.nodes[i];fprintf(f,"%s\t%s\t%s\t%s\t%s\n",n->id,n->born,n->launch,n->transaction,n->host);}
    return fclose(f)?-1:0;
}
static int sc_seal_read(void){
    struct migration_file record;memset(&record,0,sizeof record);char *copy=NULL,*expected=NULL;size_t size=0;int result=-1;
    char current_tree[65],anchor[128];
    if(sc_service_file(sc.cycles,"origin.record",&record,NULL)||sc_tree_hash(sc.op,current_tree))goto done;
    copy=strdup(record.bytes);if(!copy)goto done;char *cursor=copy,*s=sc_line(&cursor);
    if(!s||strcmp(s,"BROray-service-origin/1"))goto done;
    const char *fields[]={sc.root,sc.origin,sc.migration,sc.nonce,sc.native,sc.input.manifest,current_tree};
    for(unsigned i=0;i<sizeof fields/sizeof fields[0];i++){s=sc_line(&cursor);if(!s||strcmp(s,fields[i]))goto done;}
    strcpy(sc.tree,current_tree);s=sc_line(&cursor);if(!s||!token(s,64))goto done;strcpy(sc.initial,s);
    s=sc_line(&cursor);char *tail=NULL;unsigned long count=s?strtoul(s,&tail,10):0;
    if(!s||*tail||!count||count>=SC_LIMIT)goto done;sc.baseline=sc.count=(unsigned)count;
    unsigned selected=0;
    for(unsigned i=0;i<count;i++){
        struct sc_node *n=&sc.nodes[i];char extra;s=sc_line(&cursor);
        if(!s||sscanf(s,"%64s %63s %64s %64s %64s %c",n->id,n->born,n->launch,n->transaction,n->host,&extra)!=5||
           !token(n->id,64)||!token(n->born,36)||strlen(n->born)!=36||!hex64(n->launch)||!hex64(n->transaction)||!hex64(n->host))goto done;
        for(unsigned j=0;j<i;j++)if(!strcmp(sc.nodes[j].id,n->id))goto done;
        if(!strcmp(n->id,sc.initial)){sc.current=i;selected++;}
    }
    if(*cursor||selected!=1||sc_seal_text(&expected,&size)||size!=record.size||memcmp(expected,record.bytes,size))goto done;
    int an=snprintf(anchor,sizeof anchor,"BROray-service-origin-anchor/1\n%s\n",record.sha);
    if(an<0||an>=(int)sizeof anchor||sc_publish(sc.cycles,"origin.anchor",anchor,(size_t)an,0))goto done;
    if(sc_publish(sc.cycles,"origin.record",expected,size,0))goto done;
    strcpy(sc.seal,record.sha);for(unsigned i=0;i<sc.count;i++)strcpy(sc.nodes[i].chain,sc.seal);result=0;
 done:free(record.bytes);free(copy);free(expected);return result;
}
/* Only the first origin publication may be resumed without a transition lock.
 * Ordinary cycles, READY/STOP receipts and unknown names never enter this path.
 * Both canonical records are compared BEFORE completing either publication. */
static int sc_origin_partial_names(int fd){
    DIR *d=directory_stream(fd);if(!d)return -1;struct dirent *e;int bad=0;errno=0;
    while((e=readdir(d))){const char *n=e->d_name;
        if(strcmp(n,".")&&strcmp(n,"..")&&strcmp(n,"origin.anchor")&&strcmp(n,"origin.anchor.pending")&&
           strcmp(n,"origin.record")&&strcmp(n,"origin.record.pending")){bad=1;break;}errno=0;
    }
    if(!e&&errno)bad=1;closedir(d);return bad?-1:0;
}
static int sc_origin_compare(int fd,const char *name,const char *expected,size_t size){
    int exists=sc_record_exists(fd,name);if(exists<=0)return exists;
    struct migration_file f;memset(&f,0,sizeof f);if(sc_service_file(fd,name,&f,NULL))return -1;
    int bad=f.size!=size||memcmp(f.bytes,expected,size);free(f.bytes);return bad?-1:1;
}
static int sc_restart_incomplete(struct sc_node *n){
    if(!n->index)return 0;
    char name[128],pending[128];struct sc_record_pair pair;
    int k=snprintf(name,sizeof name,"ready-%s.record",n->id);
    if(k<0||k>=(int)sizeof name||sc_record_names(sc.cycles,name,pending,&pair))return -1;
    /* A successor intent already owns this restart. Finish it, do not try to
     * STOP an unborn/unready successor or mint yet another generation. */
    return pair.names!=1;
}

static int sc_create_seal(char **argv){
    char reply[8192],parent[PATH_MAX],start[PATH_MAX],anchor[128];char *text=NULL;size_t size=0;int starts=-1,result=-1;
    /* The old proof is used exactly once, while its completed generation is
     * still live. Future cycles never re-enter recovery-complete/retire. */
    if(sc_call(argv,"recovery-commit-check",reply,sizeof reply)||!strstr(reply,"\"phase\":\"COMMIT_VERIFIED\"")||
       !strstr(reply,"\"platformReady\":true")||sc_field(reply,"generationId",sc.initial,sizeof sc.initial)||
       sc_tree_hash(sc.op,sc.tree)||sc_join(parent,sc.uppath,"starts"))goto done;
    starts=checked_directory(parent);if(starts<0)goto done;DIR *dir=directory_stream(starts);if(!dir)goto done;
    struct dirent *e;int bad=0;errno=0;
    while((e=readdir(dir))){
        if(!strcmp(e->d_name,".")||!strcmp(e->d_name,".."))continue;
        if(sc.count>=SC_LIMIT-1||!token(e->d_name,64)||sc_join(start,parent,e->d_name)){bad=1;break;}
        int fd=checked_directory(start);struct migration_file launch,transaction,host;
        memset(&launch,0,sizeof launch);memset(&transaction,0,sizeof transaction);memset(&host,0,sizeof host);
        struct sc_node *n=&sc.nodes[sc.count];strcpy(n->id,e->d_name);
        if(fd<0||sc_file(fd,"launch.record",&launch)||sc_file(fd,"transaction.record",&transaction))bad=1;
        if(!bad){strcpy(n->launch,launch.sha);strcpy(n->transaction,transaction.sha);bad=sc_load_node(n);}
        int h=!bad?checked_directory(pl.host):-1;
        if(!bad&&(h<0||sc_file(h,"host.record",&host)||sc_field(host.bytes,"bootId",n->born,sizeof n->born)))bad=1;
        if(!bad){strcpy(n->host,host.sha);if(!strcmp(n->id,sc.initial))sc.current=sc.count;sc.count++;}
        if(fd>=0)close(fd);if(h>=0)close(h);free(launch.bytes);free(transaction.bytes);free(host.bytes);
        if(bad)break;errno=0;
    }
    if(!e&&errno)bad=1;closedir(dir);if(bad||!sc.count)goto done;sc.baseline=sc.count;
    if(sc_seal_text(&text,&size))goto done;
    if(sc.cycles<0){
        if(mkdirat(sc.up,"cycles",0700)||fsync(sc.up))goto done;
        sc.cycles=checked_directory(sc.cyclepath);
    }
    if(sc.cycles<0||sc_origin_partial_names(sc.cycles))goto done;
    digest_bytes(text,size,sc.seal);int an=snprintf(anchor,sizeof anchor,"BROray-service-origin-anchor/1\n%s\n",sc.seal);
    if(an<0||an>=(int)sizeof anchor)goto done;
    int have_anchor=sc_origin_compare(sc.cycles,"origin.anchor",anchor,(size_t)an);
    int have_record=sc_origin_compare(sc.cycles,"origin.record",text,size);
    if(have_anchor<0||have_record<0||(have_record&&!have_anchor))goto done;
    if(sc_publish(sc.cycles,"origin.anchor",anchor,(size_t)an,1)||
       sc_publish(sc.cycles,"origin.record",text,size,1)||sc_seal_read())goto done;
    result=0;
 done:if(starts>=0)close(starts);free(text);return result;
}
/* Full terminal proof; historical boot identity is never used to signal a PID.
 * It authenticates immutable ledgers plus the independently retired host. */
static int sc_terminal(struct sc_node *n,char retired_sha[65],char host_sha[65]){
    if(sc_load_node(n)||sc_host_record(n))return -1;
    int domain=checked_directory(pl.domain),host=checked_directory(pl.host),result=-1;
    struct retired_record r;struct migration_file end,host_record;memset(&end,0,sizeof end);memset(&host_record,0,sizeof host_record);
    char name[64],*body=NULL,expected[512];size_t size=0;
    if(domain<0||host<0||flock(host,LOCK_EX|LOCK_NB)||retirement_valid(domain,pl.domain,&r)||strcmp(r.gen,n->id)||strcmp(r.sha,sc.input.manifest)||
       sc_file(domain,"retirement.receipt",&end))goto done;
    record_name(r.total,name);if(safe_bytes_at(domain,name,&body,&size)||size>=sizeof snapshot||memchr(body,0,size))goto done;
    memcpy(snapshot,body,size);snapshot[size]=0;char born[64];if(sc_field(snapshot,"bootId",born,sizeof born)||strcmp(born,n->born))goto done;
    int en=snprintf(expected,sizeof expected,"\"startIntentSha256\":\"%s\"",n->launch);if(en<0||en>=(int)sizeof expected||!strstr(snapshot,expected))goto done;
    en=snprintf(expected,sizeof expected,"\"transactionRecordSha256\":\"%s\"",n->transaction);if(en<0||en>=(int)sizeof expected||!strstr(snapshot,expected))goto done;
    en=snprintf(expected,sizeof expected,"\"serviceHostRecordSha256\":\"%s\"",n->host);if(en<0||en>=(int)sizeof expected||!strstr(snapshot,expected)||
       sc_file(host,"host.record",&host_record)||strcmp(host_record.sha,n->host))goto done;
    char *args[]={pl.nativepath,"service-host",pl.host,pl.domain,n->id,pl.manifest,pl.root,pl.shell,pl.shellsha,NULL};
    char terminal_text[512];if(service_retirement_text_at(host,args,host_record.bytes,host_record.size,terminal_text,n->born)||
       bg_record_exact(host,"retirement.receipt",terminal_text,strlen(terminal_text)))goto done;
    strcpy(retired_sha,end.sha);digest_bytes(terminal_text,strlen(terminal_text),host_sha);result=0;
 done:if(domain>=0)close(domain);if(host>=0)close(host);free(end.bytes);free(host_record.bytes);free(body);return result;
}
static int sc_identity_slice(const char *text,const char **begin,size_t *size){
    const char *p=strstr(text,",\"supervisor\":"),*end=p?strstr(p,",\"stopOperationId\":"):NULL;
    if(!p||!end||end<=p||!strstr(p,",\"updater\":{\"pid\":"))return -1;*begin=p;*size=(size_t)(end-p);return 0;
}
static const char *sc_live_reason="not-checked";
static int sc_live_failure(const char *why){sc_live_reason=why;return -1;}
static int sc_live(struct sc_node *n){
    if(strcmp(n->born,boot)||sc_load_node(n))return sc_live_failure("launch-or-boot");
    char *args[]={pl.nativepath,"control",pl.domain,"STATUS",pl.id,pl.manifest,sc.origin,sc.nonce,NULL};
    if(control_exchange(8,args,0)||!strstr(snapshot,"\"state\":\"RUNNING\",\"supervisedFromBirth\":true,")||
       !strstr(snapshot,"\"platformReady\":true,\"platformLaunch\":{\"contract\":\"broray-platform-launch/1\","))return sc_live_failure("authenticated-running-ready");
    char field[512],observed[65];
    const char *keys[]={"startIntentSha256","transactionRecordSha256","nativeSha256","daemonSha256","interpreterSha256"};
    const char *values[]={n->launch,n->transaction,sc.native,pl.files[5].sha,pl.shellsha};
    for(unsigned i=0;i<5;i++){int k=snprintf(field,sizeof field,"\"%s\":\"%s\"",keys[i],values[i]);if(k<0||k>=(int)sizeof field||!strstr(snapshot,field))return sc_live_failure("platform-byte-binding");}
    if(sc_field(snapshot,"serviceHostRecordSha256",observed,sizeof observed)||!hex64(observed)||(n->host[0]&&strcmp(n->host,observed)))return sc_live_failure("host-record-binding");
    strcpy(n->host,observed);struct identity host;
    char *hostargs[]={pl.nativepath,"service-status",pl.host,pl.domain,pl.id,pl.manifest,pl.root,pl.shell,pl.shellsha,NULL};
    if(service_host_probe(hostargs,&host)||service_host_record_sha(hostargs,&host,observed)||strcmp(observed,n->host))return sc_live_failure("host-live-identity");
    const char *u=strstr(snapshot,",\"updater\":");long pid;struct identity now;
    if(!u||sscanf(u,",\"updater\":{\"pid\":%ld,",&pid)!=1||pid<=1||pid>INT_MAX||capture((pid_t)pid,&now)||strcmp(now.exe,pl.shell))return sc_live_failure("updater-live-identity");
    char *identity=NULL;size_t size=0;FILE *f=open_memstream(&identity,&size);if(!f)return sc_live_failure("identity-buffer");identity_json(f,&now);
    if(fclose(f)){free(identity);return sc_live_failure("identity-serialization");}int bad=strncmp(u+11,identity,size);free(identity);if(bad)return sc_live_failure("updater-ledger-identity");
    struct identity saved=updater;updater=now;bad=pl_queue_ready();updater=saved;
    if(bad||pl_exact())return sc_live_failure("queue-or-platform-final-check");sc_live_reason="ready";return 0;
}
static int sc_ready(struct sc_node *n,int publish,char sha[65]){
    char name[128],prefix[512],*text=NULL;size_t size=0;struct migration_file saved;memset(&saved,0,sizeof saved);int result=-1,domain=-1;
    int nn=snprintf(name,sizeof name,"ready-%s.record",n->id);
    int pn=snprintf(prefix,sizeof prefix,"BROray-service-ready/1\n%s\n%s\n%s\n%s\n",sc.seal,n->launch,n->transaction,n->host);
    if(nn<0||nn>=(int)sizeof name||pn<0||pn>=(int)sizeof prefix)return -1;
    int present=sc_record_exists(sc.cycles,name);if(present<0||(!present&&!publish))return -1;
    if(!present){
        if(sc_live(n))return -1;
        FILE *f=open_memstream(&text,&size);if(!f)return -1;
        /* Pin one authentic READY revision outside the changing ledger. */
        fprintf(f,"BROray-service-ready/1\n%s\n%s\n%s\n%s\n",sc.seal,n->launch,n->transaction,n->host);fputs(snapshot,f);
        if(fclose(f)||sc_publish(sc.cycles,name,text,size,1))goto done;
        pn=snprintf(prefix,sizeof prefix,"BROray-service-ready/1\n%s\n%s\n%s\n%s\n",sc.seal,n->launch,n->transaction,n->host);
    }
    if(sc_service_file(sc.cycles,name,&saved,NULL)||saved.size<=(size_t)pn||memcmp(saved.bytes,prefix,(size_t)pn))goto done;
    const char *accepted=saved.bytes+pn,*a,*b;size_t as,bs;char born[64],id[65];unsigned long rev=0;
    const char *v=strstr(accepted,",\"revision\":");
    if(!v||sscanf(v,",\"revision\":%lu,",&rev)!=1||!rev||rev>1000000||
       sc_field(accepted,"generationId",id,sizeof id)||strcmp(id,n->id)||sc_field(accepted,"bootId",born,sizeof born)||strcmp(born,n->born)||
       !strstr(accepted,"\"platformReady\":true")||sc_identity_slice(accepted,&a,&as)||sc_identity_slice(snapshot,&b,&bs)||as!=bs||memcmp(a,b,as))goto done;
    domain=checked_directory(pl.domain);if(domain<0)goto done;char record[64];record_name(rev,record);
    if(bg_record_exact(domain,record,accepted,saved.size-(size_t)pn))goto done;
    if(sc_publish(sc.cycles,name,saved.bytes,saved.size,0))goto done;
    strcpy(sha,saved.sha);result=0;
 done:free(text);free(saved.bytes);if(domain>=0)close(domain);return result;
}
static int sc_stop_tree(struct sc_node *n,char sha[65]){
    if(sc_load_node(n))return -1;int domain=checked_directory(pl.domain);if(domain<0)return -1;struct retired_record r;
    int bad=retirement_valid(domain,pl.domain,&r);close(domain);if(bad||strncmp(r.op,"op-",3))return -1;
    char path[PATH_MAX],parent[PATH_MAX];if(sc_join(parent,sc.statepath,"operations")||sc_join(path,parent,r.op))return -1;
    int fd=checked_directory(path);if(fd<0)return -1;bad=sc_tree_hash_at(fd,path,sha);close(fd);return bad;
}
static int sc_derive(struct sc_node *previous,const char *born,unsigned index,struct sc_node *next,char **record,size_t *record_size,char **launch,size_t *launch_size,char **transaction,size_t *transaction_size){
    char retired[65],host[65],ready[65],stop_tree[65],seed[1024],seed_sha[65],domain[PATH_MAX],parent[PATH_MAX];
    struct migration_file manifest;memset(&manifest,0,sizeof manifest);int result=-1;
    if(sc_predecessor(previous,retired,host,ready,stop_tree)||
       sc_file(sc.op,"platform-migration/manifest.record",&manifest)||strcmp(manifest.sha,sc.input.manifest))goto done;
    int sn=snprintf(seed,sizeof seed,"BROray-service-generation/1\n%s\n%u\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n",sc.seal,index,previous->id,previous->chain,retired,host,ready,stop_tree,born);
    if(sn<0||sn>=(int)sizeof seed)goto done;digest_bytes(seed,(size_t)sn,seed_sha);
    memset(next,0,sizeof *next);platform_generation_id(seed_sha,next->id);strcpy(next->born,born);next->index=index;
    if(!strcmp(next->id,previous->id)||sc_join(parent,sc.uppath,"generations")||sc_join(domain,parent,next->id))goto done;
    FILE *f=open_memstream(launch,launch_size);if(!f)goto done;
    fprintf(f,"BROray-platform-launch/2\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n",sc.root,domain,next->id,sc.input.manifest,sc.native,pl.shell,pl.shellsha,sc.origin,sc.nonce);
    fwrite(manifest.bytes,1,manifest.size,f);if(fclose(f))goto done;digest_bytes(*launch,*launch_size,next->launch);
    f=open_memstream(transaction,transaction_size);if(!f)goto done;
    fprintf(f,"BROray-service-start-intent/1\nSTART_INTENT\n%s\n%s\n%s\n%s\n",seed_sha,born,sc.seal,next->launch);fwrite(seed,1,(size_t)sn,f);
    if(fclose(f))goto done;digest_bytes(*transaction,*transaction_size,next->transaction);
    f=open_memstream(record,record_size);if(!f)goto done;
    fprintf(f,"BROray-service-cycle/1\n%u\n%s\n%s\n%s\n%s\n%s\n",index,born,next->id,next->launch,next->transaction,seed_sha);
    if(fclose(f))goto done;digest_bytes(*record,*record_size,next->chain);result=0;
 done:free(manifest.bytes);return result;
}
static int sc_ready_host(struct sc_node *n){
    char name[128];int nn=snprintf(name,sizeof name,"ready-%s.record",n->id);if(nn<0||nn>=(int)sizeof name)return -1;
    int exists=sc_record_exists(sc.cycles,name);if(exists<=0)return exists;
    struct migration_file f;memset(&f,0,sizeof f);int result=-1;
    if(sc_service_file(sc.cycles,name,&f,NULL))goto done;char *cursor=f.bytes,*line=sc_line(&cursor);
    if(!line||strcmp(line,"BROray-service-ready/1"))goto done;
    const char *values[]={sc.seal,n->launch,n->transaction};for(unsigned i=0;i<3;i++){line=sc_line(&cursor);if(!line||strcmp(line,values[i]))goto done;}
    line=sc_line(&cursor);if(!line||!hex64(line)||(n->host[0]&&strcmp(n->host,line)))goto done;strcpy(n->host,line);result=0;
 done:free(f.bytes);return result;
}
static int sc_namespace(const char *family,int allow_missing_current){
    char path[PATH_MAX];if(sc_join(path,sc.uppath,family))return -1;int fd=checked_directory(path);if(fd<0)return -1;
    DIR *d=directory_stream(fd);if(!d){close(fd);return -1;}struct dirent *e;unsigned char seen[SC_LIMIT]={0};int bad=0;errno=0;
    while((e=readdir(d))){
        if(!strcmp(e->d_name,".")||!strcmp(e->d_name,".."))continue;
        struct stat st;if(!strcmp(family,"generations")&&!strcmp(e->d_name,".generation-lifetime.lock")){
            if(fstatat(fd,e->d_name,&st,AT_SYMLINK_NOFOLLOW)||!S_ISREG(st.st_mode)||st.st_uid!=geteuid()||(st.st_mode&07777)!=0600||st.st_nlink!=1||st.st_size){bad=1;break;}errno=0;continue;
        }
        int at=-1;for(unsigned i=0;i<sc.count;i++)if(!strcmp(sc.nodes[i].id,e->d_name))at=(int)i;
        if(at<0||seen[at]++||fstatat(fd,e->d_name,&st,AT_SYMLINK_NOFOLLOW)||!S_ISDIR(st.st_mode)||st.st_uid!=geteuid()||(st.st_mode&07777)!=0700){bad=1;break;}errno=0;
    }
    if(!e&&errno)bad=1;closedir(d);close(fd);
    for(unsigned i=0;i<sc.count;i++)if(!seen[i]&&!(allow_missing_current&&i==sc.current))bad=1;
    return bad?-1:0;
}
static int sc_cycle_names(unsigned cycles){
    DIR *d=directory_stream(sc.cycles);if(!d)return -1;struct dirent *e;int bad=0;errno=0;
    while((e=readdir(d))){const char *name=e->d_name;if(!strcmp(name,".")||!strcmp(name,".."))continue;
        char plain[128];if(strlen(name)>=sizeof plain){bad=1;break;}strcpy(plain,name);
        size_t length=strlen(plain);if(length>8&&!strcmp(plain+length-8,".pending"))plain[length-8]=0;
        name=plain;
        int allowed=!strcmp(name,"origin.record")||!strcmp(name,"origin.anchor")||!strcmp(name,"transition.lock");char want[128];
        for(unsigned i=1;!allowed&&i<=cycles;i++){snprintf(want,sizeof want,"cycle-%020u.record",i);allowed=!strcmp(name,want);}
        for(unsigned i=0;!allowed&&i<sc.count;i++){snprintf(want,sizeof want,"ready-%s.record",sc.nodes[i].id);allowed=!strcmp(name,want);if(!allowed){snprintf(want,sizeof want,"stopped-%s.record",sc.nodes[i].id);allowed=!strcmp(name,want);}}
        if(!allowed&&!strncmp(name,"boot-residue-",13)){
            int index=-1;for(unsigned i=0;i<sc.count;i++)if(!strcmp(name+13,sc.nodes[i].id))index=(int)i;
            struct stat st;struct gb_record ended;char proof[65];
            if(index<0||strcmp(name,e->d_name)||sc_boot_proof(&sc.nodes[index],&ended,proof)||
               fstatat(sc.cycles,name,&st,AT_SYMLINK_NOFOLLOW)||!S_ISDIR(st.st_mode)||(st.st_mode&07777)!=0700||st.st_uid!=geteuid()){bad=1;break;}
            errno=0;continue;
        }
        char pending[128];struct sc_record_pair pair;
        if(!allowed||sc_record_names(sc.cycles,name,pending,&pair)||!pair.names||
           (!strcmp(name,"transition.lock")&&(pair.names!=1||strcmp(name,e->d_name)||pair.identity.st_size))){bad=1;break;}errno=0;
    }
    if(!e&&errno)bad=1;closedir(d);return bad?-1:0;
}
static int sc_collect(void){
    if(sc_seal_read())return -1;unsigned index=1;
    for(;sc.count<SC_LIMIT;index++){
        char name[80];snprintf(name,sizeof name,"cycle-%020u.record",index);int present=sc_record_exists(sc.cycles,name);if(present<0)return -1;if(!present)break;
        struct migration_file f;memset(&f,0,sizeof f);char *copy=NULL,*record=NULL,*launch=NULL,*transaction=NULL;size_t rs=0,ls=0,ts=0;int bad=1;
        struct sc_node next;
        if(!sc_service_file(sc.cycles,name,&f,NULL)){
            copy=strdup(f.bytes);if(copy){char *cursor=copy,*magic=sc_line(&cursor),*number=sc_line(&cursor),*born=sc_line(&cursor),expected[32];snprintf(expected,sizeof expected,"%u",index);
                if(magic&&number&&born&&!strcmp(magic,"BROray-service-cycle/1")&&!strcmp(number,expected)&&strlen(born)==36&&token(born,36)&&
                   !sc_derive(&sc.nodes[sc.current],born,index,&next,&record,&rs,&launch,&ls,&transaction,&ts)&&rs==f.size&&!memcmp(record,f.bytes,rs)&&!sc_publish(sc.cycles,name,record,rs,0))bad=0;
            }
        }
        if(!bad){for(unsigned i=0;i<sc.count;i++)if(!strcmp(next.id,sc.nodes[i].id))bad=1;}
        if(!bad){sc.current=sc.count;sc.nodes[sc.count++]=next;bad=sc_ready_host(&sc.nodes[sc.current]);}
        free(f.bytes);free(copy);free(record);free(launch);free(transaction);if(bad)return -1;
    }
    if(sc.count>=SC_LIMIT||sc_cycle_names(index-1)||sc_namespace("starts",1)||sc_namespace("generations",1)||sc_namespace("hosts",1))return -1;
    for(unsigned i=0;i<sc.count;i++)if(i!=sc.current){char a[65],b[65];struct gb_record ended;if(sc_terminal(&sc.nodes[i],a,b)&&(sc_boot_proof(&sc.nodes[i],&ended,a)||sc_boot_markers(&sc.nodes[i],0)))return -1;}
    return 0;
}
static int sc_directory(int parent,const char *name,const char *path,int create){
    int exists=bg_exists(parent,name);if(exists<0||(!exists&&!create))return -1;
    if(!exists&&(mkdirat(parent,name,0700)||fsync(parent)))return -1;
    int fd=checked_directory(path);struct stat held,named;if(fd<0)return -1;
    if(fstat(fd,&held)||fstatat(parent,name,&named,AT_SYMLINK_NOFOLLOW)||held.st_dev!=named.st_dev||held.st_ino!=named.st_ino){close(fd);return -1;}return fd;
}
static int sc_boot_proof(struct sc_node *n,struct gb_record *b,char sha[65]){
    if(sc_load_node(n))return -1;int fd=checked_directory(pl.domain);struct migration_file f;memset(&f,0,sizeof f);int rc=-1;
    if(fd<0||gb_validate(fd,pl.domain,b)||strcmp(b->id,n->id)||strcmp(b->manifest,sc.input.manifest)||strcmp(b->seal,sc.seal)||
       strcmp(b->launch,n->launch)||strcmp(b->transaction,n->transaction)||strcmp(b->from,n->born)||
       (n->host[0]&&strcmp(b->host,n->host))||sc_file(fd,"boot-ended.receipt",&f))goto done;
    strcpy(n->host,b->host);strcpy(sha,f.sha);rc=0;
 done:if(fd>=0)close(fd);free(f.bytes);return rc;
}
static int sc_predecessor(struct sc_node *n,char retired[65],char host[65],char ready[65],char tree[65]){
    struct gb_record b;
    if(!sc_boot_proof(n,&b,retired)){if(sc_boot_markers(n,0))return -1;strcpy(host,b.journal);strcpy(ready,b.ready);strcpy(tree,b.inventory);return 0;}
    return sc_stopped_seal(n,0)||sc_terminal(n,retired,host)||sc_ready(n,0,ready)||sc_stop_tree(n,tree)?-1:0;
}
static int sc_boot_retire(struct sc_node *n){
    if(!strcmp(n->born,boot)||sc_load_node(n)||sc_host_record(n))return -1;
    int domain=checked_directory(pl.domain),host=checked_directory(pl.host),gens=-1,whole=-1,life=-1,result=-1;
    char path[PATH_MAX],name[128],text[2048],current[64];struct gb_record b;memset(&b,0,sizeof b);
    struct migration_file r,saved;memset(&r,0,sizeof r);memset(&saved,0,sizeof saved);
    if(domain<0||host<0||sc_join(path,sc.uppath,"generations"))goto done;gens=checked_directory(path);
    if(gens<0||(whole=sc_lock_exact(gens,".generation-lifetime.lock",0))<0||
       (life=sc_lock_exact(domain,"lifetime.lock",0))<0||flock(host,LOCK_EX|LOCK_NB)||migration_boot(current)||!strcmp(current,n->born))goto done;
    strcpy(b.id,n->id);strcpy(b.manifest,sc.input.manifest);strcpy(b.from,n->born);strcpy(b.through,current);
    strcpy(b.seal,sc.seal);strcpy(b.host,n->host);strcpy(b.launch,n->launch);strcpy(b.transaction,n->transaction);scope_digest(pl.domain,b.scope);
    int k=snprintf(name,sizeof name,"ready-%s.record",n->id);
    if(k<0||k>=(int)sizeof name||sc_file(sc.cycles,name,&r))goto done;strcpy(b.ready,r.sha);
    int exists=sc_record_exists(domain,"boot-ended.receipt");if(exists<0)goto done;
    if(exists){
        if(sc_service_file(domain,"boot-ended.receipt",&saved,NULL))goto done;
        char *copy=strdup(saved.bytes);if(!copy)goto done;char *cursor=copy,*row=NULL;
        for(int i=0;i<5;i++){row=sc_line(&cursor);if(!row)break;}
        int valid=row&&strlen(row)==36&&token(row,36)&&strcmp(row,n->born);if(valid)strcpy(b.through,row);free(copy);if(!valid)goto done;
    }
    if(gb_measure(domain,pl.domain,&b))goto done;k=gb_text(&b,text);
    if(k<0||k>=2048||sc_publish(domain,"boot-ended.receipt",text,(size_t)k,1)||gb_validate(domain,pl.domain,NULL))goto done;
    result=0;
 done:if(life>=0)close(life);if(whole>=0)close(whole);if(gens>=0)close(gens);if(host>=0)close(host);if(domain>=0)close(domain);free(r.bytes);free(saved.bytes);return result;
}
/* Preserve old daemon projections under their own boot receipt instead of
 * deleting them. Only exact, private, boot-ended markers may move. A foreign
 * projection, symlink, changed inode or unknown residue fences the transition.
 */
static int sc_boot_markers(struct sc_node *n,int finish){
    struct gb_record b;char proof[65],name[128],path[PATH_MAX],expected[32];struct migration_file r;memset(&r,0,sizeof r);
    int archive=-1,result=-1;if(sc_boot_proof(n,&b,proof))goto done;
    int k=snprintf(name,sizeof name,"ready-%s.record",n->id);if(k<0||k>=(int)sizeof name||sc_file(sc.cycles,name,&r))goto done;
    const char *u=strstr(r.bytes,",\"updater\":{\"pid\":");long pid;
    if(!u||sscanf(u,",\"updater\":{\"pid\":%ld,",&pid)!=1||pid<=1||pid>INT_MAX)goto done;
    k=snprintf(expected,sizeof expected,"%ld\n",pid);if(k<=0||k>=(int)sizeof expected)goto done;
    int nn=snprintf(name,sizeof name,"boot-residue-%s",n->id);if(nn<0||nn>=(int)sizeof name||sc_join(path,sc.cyclepath,name))goto done;
    archive=sc_directory(sc.cycles,name,path,finish);if(archive<0)goto done;
    DIR *d=directory_stream(archive);if(!d)goto done;struct dirent *e;int bad=0;errno=0;
    while((e=readdir(d))){if(strcmp(e->d_name,".")&&strcmp(e->d_name,"..")&&strcmp(e->d_name,"daemon.pid")&&strcmp(e->d_name,"daemon.ready")&&strcmp(e->d_name,"daemon.lock")){bad=1;break;}errno=0;}
    if(!e&&errno)bad=1;closedir(d);if(bad)goto done;
    const char *files[]={"daemon.pid","daemon.ready","daemon.lock"};
    for(unsigned i=0;i<3;i++){
        int a=finish?bg_exists(sc.up,files[i]):0,z=bg_exists(archive,files[i]);if(a<0||z<0||a+z!=1)goto done;
        int dir=a?sc.up:archive;struct stat before,after;if(fstatat(dir,files[i],&before,AT_SYMLINK_NOFOLLOW)||before.st_uid!=geteuid())goto done;
        if(i<2){if(bg_record_exact(dir,files[i],expected,(size_t)k))goto done;}
        else{
            int lock=openat(dir,files[i],O_RDONLY|O_DIRECTORY|O_NOFOLLOW|O_CLOEXEC);
            if(lock<0)goto done;bad=!S_ISDIR(before.st_mode)||(before.st_mode&07777)!=0700||bg_empty(lock);close(lock);if(bad)goto done;
        }
        if(a){
            if(syscall(SYS_renameat2,sc.up,files[i],archive,files[i],1)||fsync(sc.up)||fsync(archive)||
               fstatat(archive,files[i],&after,AT_SYMLINK_NOFOLLOW)||!sc_same_record(&before,&after)||after.st_nlink!=before.st_nlink)goto done;
        }
    }
    result=0;
 done:if(archive>=0)close(archive);free(r.bytes);return result;
}

static int sc_intent_publish(struct sc_node *previous){
    char *record=NULL,*launch=NULL,*transaction=NULL;size_t rs=0,ls=0,ts=0;struct sc_node next;char name[80];
    unsigned index=sc.count-sc.baseline+1;int result=-1;
    if(sc.count>=SC_LIMIT-1||sc_derive(previous,boot,index,&next,&record,&rs,&launch,&ls,&transaction,&ts))goto done;
    snprintf(name,sizeof name,"cycle-%020u.record",index);
    /* This immutable, chained intent is the sole retry key. It is durable
     * BEFORE any host/generation directory or process can be created. */
    if(bg_exists(sc.cycles,name)!=0||sc_publish(sc.cycles,name,record,rs,1))goto done;
    sc.current=sc.count;sc.nodes[sc.count++]=next;result=0;
 done:free(record);free(launch);free(transaction);return result;
}
static int sc_materialize(struct sc_node *n){
    if(!n->index)return sc_load_node(n);struct sc_node derived;
    char *record=NULL,*launch=NULL,*transaction=NULL;size_t rs=0,ls=0,ts=0;char parent[PATH_MAX],path[PATH_MAX],name[80];int starts=-1,fd=-1,result=-1;
    struct sc_node *previous=n->index==1?NULL:&sc.nodes[sc.current-1];
    if(!previous)for(unsigned i=0;i<sc.baseline;i++)if(!strcmp(sc.nodes[i].id,sc.initial))previous=&sc.nodes[i];
    if(!previous||sc_derive(previous,n->born,n->index,&derived,&record,&rs,&launch,&ls,&transaction,&ts)||strcmp(derived.id,n->id))goto done;
    snprintf(name,sizeof name,"cycle-%020u.record",n->index);
    if(sc_publish(sc.cycles,name,record,rs,0)||sc_join(parent,sc.uppath,"starts")||sc_join(path,parent,n->id))goto done;
    starts=checked_directory(parent);if(starts<0)goto done;fd=sc_directory(starts,n->id,path,1);if(fd<0)goto done;
    char generations[PATH_MAX],hosts[PATH_MAX],domain[PATH_MAX],host[PATH_MAX];
    if(sc_join(generations,sc.uppath,"generations")||sc_join(hosts,sc.uppath,"hosts")||sc_join(domain,generations,n->id)||sc_join(host,hosts,n->id))goto done;
    struct stat st;int born=lstat(domain,&st)==0||lstat(host,&st)==0;
    if(sc_publish(fd,"transaction.record",transaction,ts,!born)||sc_publish(fd,"launch.record",launch,ls,!born)||
       sc_load_node(n))goto done;
    result=0;
 done:if(starts>=0)close(starts);if(fd>=0)close(fd);free(record);free(launch);free(transaction);return result;
}
static int sc_stopped_seal(struct sc_node *n,int publish){
    char retired[65],host[65],ready[65],tree[65],record[640],name[128];
    if(sc_terminal(n,retired,host)||sc_ready(n,0,ready)||sc_stop_tree(n,tree))return -1;
    int len=snprintf(record,sizeof record,"BROray-service-stopped/1\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n",sc.seal,n->id,n->born,n->chain,retired,host,ready,tree);
    int nn=snprintf(name,sizeof name,"stopped-%s.record",n->id);
    if(len<0||len>=(int)sizeof record||nn<0||nn>=(int)sizeof name)return -1;
    return publish?sc_publish(sc.cycles,name,record,(size_t)len,1):sc_publish(sc.cycles,name,record,(size_t)len,0);
}
static int sc_log(int dir,const char *name){
    int fd=openat(dir,name,O_WRONLY|O_APPEND|O_CREAT|O_NOFOLLOW|O_CLOEXEC,0600);struct stat st;
    if(fd<0)return -1;if(fstat(fd,&st)||!S_ISREG(st.st_mode)||st.st_uid!=geteuid()||st.st_nlink!=1||(st.st_mode&07777)!=0600||fsync(fd)||fsync(dir)){close(fd);return -1;}return fd;
}
static int sc_fork_service(struct sc_node *n,int log,int host,const char *host_sha){
    char canonical[PATH_MAX],sha[65];struct stat st;
    int length=snprintf(canonical,sizeof canonical,"%s/runtimes/%s/runtime",sc.uppath,sc.native);
    if(length<0||length>=(int)sizeof canonical)return -1;
    int root=migration_directory("/");if(root<0)return -1;
    int runtime=migration_relative(root,canonical+1);close(root);
    if(runtime<0)return -1;
    if(fstat(runtime,&st)||!S_ISREG(st.st_mode)||st.st_uid!=geteuid()||st.st_nlink!=1||(st.st_mode&07777)!=0700||hash_fd(runtime,sha)||strcmp(sha,sc.native)){close(runtime);return -1;}
    pid_t child=fork();if(child<0){close(runtime);return -1;}
    if(!child){
        if(setsid()<0||dup2(log,1)<0||dup2(log,2)<0)_exit(74);
        int null=open("/dev/null",O_RDONLY|O_CLOEXEC);if(null<0||dup2(null,0)<0)_exit(74);
        service_close_fds(runtime,host?-1:sc.transition);if(clearenv())_exit(74);
        if(!host){char fd[32];snprintf(fd,sizeof fd,"%d",sc.transition);int flags=fcntl(sc.transition,F_GETFD);
            if(flags<0||fcntl(sc.transition,F_SETFD,flags&~FD_CLOEXEC)||setenv("BRORAY_SERVICE_TRANSITION_FD",fd,1))_exit(74);}
        char *ha[]={canonical,"service-host",pl.host,pl.domain,n->id,pl.manifest,pl.root,pl.shell,pl.shellsha,NULL};
        char *ga[]={canonical,"run",pl.domain,n->id,pl.manifest,"--",canonical,"platform-daemon",pl.startdir,n->launch,n->transaction,(char*)host_sha,NULL};
        extern char **environ;fexecve(runtime,host?ha:ga,environ);_exit(74);
    }
    close(runtime);return 0;
}
/* A cheap, untrusted wake-up hint from the generation's newest published
 * revision. daemon.ready precedes the traced READY handshake and is therefore
 * too early to start repeated full STATUS verification. This read authorizes
 * nothing: every positive hint is followed by unchanged sc_live/sc_ready
 * authentication, complete ledger/manifest checks and process identity proof.
 * Missing, forged or rolled-back hints can only delay a check or trigger a
 * full refusal; they never publish evidence or authorize another generation.
 */
static int sc_ready_wakeup(int domain,const struct sc_node *n){
    DIR *d=directory_stream(domain);if(!d)return -1;
    struct dirent *e;unsigned long last=0;int bad=0;errno=0;
    while((e=readdir(d))){
        unsigned long revision=0;char extra,canonical[64];
        if(!strcmp(e->d_name,"state.json"))revision=1;
        else if(sscanf(e->d_name,"revision-%20lu.json%c",&revision,&extra)==1&&revision>=2&&revision<=1000000){
            record_name(revision,canonical);if(strcmp(e->d_name,canonical))revision=0;
        }else revision=0;
        if(revision>last)last=revision;errno=0;
    }
    if(errno)bad=1;closedir(d);if(bad)return -1;if(!last)return 0;
    char name[64],field[160];struct migration_file file;memset(&file,0,sizeof file);record_name(last,name);
    /* An in-progress publisher may temporarily have both hardlink names.
     * Do not repair it, and do not poll the supervisor until publication ends. */
    if(migration_read(domain,name,&file,0))return 0;
    int hint=file.size<SNAPSHOT_LIMIT&&!memchr(file.bytes,0,file.size)&&
        strstr(file.bytes,"\"state\":\"RUNNING\",\"supervisedFromBirth\":true,")&&strstr(file.bytes,"\"platformReady\":true,");
    int k=snprintf(field,sizeof field,"\"generationId\":\"%s\"",n->id);
    hint=hint&&k>0&&k<(int)sizeof field&&strstr(file.bytes,field);
    free(file.bytes);return hint?1:0;
}

static int sc_start(struct sc_node *n){
    if(strcmp(n->born,boot)||sc_materialize(n)||sc_load_node(n))return -1;
    int start=checked_directory(pl.startdir),hosts=-1,host=-1,gens=-1,domain=-1,log=-1,result=-1;
    char path[PATH_MAX],hostsha[65];struct identity owner;
    if(start<0||sc_join(path,sc.uppath,"hosts"))goto done;hosts=checked_directory(path);if(hosts<0)goto done;
    host=sc_directory(hosts,n->id,pl.host,1);if(host<0)goto done;
    char *args[]={pl.nativepath,"service-status",pl.host,pl.domain,n->id,pl.manifest,pl.root,pl.shell,pl.shellsha,NULL};
    if(bg_exists(host,"host.record")==0){
        /* A busy/partially initialized host is retained, not adopted or killed. */
        if(flock(host,LOCK_EX|LOCK_NB)||bg_empty(host))goto done;
        if(flock(host,LOCK_UN))goto done;log=sc_log(start,"service-host.log");if(log<0||sc_fork_service(n,log,1,NULL))goto done;close(log);log=-1;
    }
    uint64_t until=millis()+2000;int verified=0;
    do{if(!service_host_probe(args,&owner)&&!service_host_record_sha(args,&owner,hostsha)){verified=1;break;}
       struct timespec pause={0,20000000L};nanosleep(&pause,NULL);}while(millis()<until);
    if(!verified||(n->host[0]&&strcmp(n->host,hostsha)))goto done;strcpy(n->host,hostsha);
    if(sc_join(path,sc.uppath,"generations"))goto done;gens=checked_directory(path);if(gens<0)goto done;
    domain=sc_directory(gens,n->id,pl.domain,1);if(domain<0)goto done;
    int state_present=bg_exists(domain,"state.json");if(state_present<0)goto done;
    if(!state_present){
        /* run publishes its first immutable ledger before birthing the daemon.
         * A nonempty incomplete domain is never recycled as a fresh launch. */
        if(bg_empty(domain))goto done;log=sc_log(start,"supervisor.log");if(log<0||sc_fork_service(n,log,0,hostsha))goto done;close(log);log=-1;
    }
    uint64_t wait_begin=millis(),poll_ms=0;unsigned polls=0;
    until=wait_begin+60000;verified=0;
    sc_live_reason="readiness-not-observed";
    do{
        int hint=sc_ready_wakeup(domain,n);if(hint<0)goto done;
        if(hint){
            uint64_t began=millis();int bad=sc_live(n);poll_ms+=millis()-began;polls++;
            if(!bad){verified=1;break;}
        }
        struct timespec pause={0,50000000L};nanosleep(&pause,NULL);
    }while(millis()<until);
    fprintf(stderr,"SERVICE_START_WAIT ready=%d elapsed_ms=%llu polls=%u poll_ms=%llu wakeup=published-ready-revision\n",verified,(unsigned long long)(millis()-wait_begin),polls,(unsigned long long)poll_ms);
    if(!verified){fprintf(stderr,"SERVICE_READY_REJECTED=%s\n",sc_live_reason);goto done;}char receipt[65];if(sc_ready(n,1,receipt)||sc_live(n))goto done;result=0;
 done:if(start>=0)close(start);if(hosts>=0)close(hosts);if(host>=0)close(host);if(gens>=0)close(gens);if(domain>=0)close(domain);if(log>=0)close(log);return result;
}
static int sc_inherited_lock(int parent,const char *name){
    struct stat named;if(fstatat(parent,name,&named,AT_SYMLINK_NOFOLLOW)||!S_ISREG(named.st_mode)||named.st_uid!=geteuid()||named.st_nlink!=1||(named.st_mode&07777)!=0600||named.st_size)return -1;
    DIR *d=opendir("/proc/self/fd");if(!d)return -1;struct dirent *e;int found=-1;
    while((e=readdir(d))){char *tail;long candidate=strtol(e->d_name,&tail,10);if(*tail||candidate<3||candidate>INT_MAX||candidate==dirfd(d))continue;
        struct stat st;int flags=fcntl((int)candidate,F_GETFL);
        if(flags<0||(flags&O_ACCMODE)!=O_RDWR||fstat((int)candidate,&st)||st.st_dev!=named.st_dev||st.st_ino!=named.st_ino)continue;
        if(flock((int)candidate,LOCK_EX|LOCK_NB))break;found=fcntl((int)candidate,F_DUPFD_CLOEXEC,3);break;
    }
    closedir(d);return found;
}
static int sc_pending_global(void){
    char path[PATH_MAX];const char *prefix=!strcmp(sc.root,"/")?"":sc.root;
    int n=snprintf(path,sizeof path,"%s/opt/var/lock/broray/global-operation.lock",prefix);struct stat st;
    if(n<0||n>=(int)sizeof path)return -1;if(lstat(path,&st)==0)return 1;return errno==ENOENT?0:-1;
}
static void sc_close(void){
    int *fds[]={&sc.transition,&sc.cycles,&sc.up,&sc.op,&sc.guard,&sc.state};
    for(unsigned i=0;i<sizeof fds/sizeof fds[0];i++)if(*fds[i]>=0){close(*fds[i]);*fds[i]=-1;}
    free(sc.input.intent);sc.input.intent=NULL;
}
static int service_cycle_available(const char *root){
    char path[PATH_MAX];const char *prefix=strcmp(root,"/")?root:"";struct stat st;
    int n=snprintf(path,sizeof path,"%s/opt/var/lib/broray-updater/cycles",prefix);
    if(n<0||n>=(int)sizeof path)return -1;if(lstat(path,&st)==0)return 1;return errno==ENOENT?0:-1;
}
static int service_cycle_main(int argc,char **argv,int action){
    int result=75,fresh=0,mig=-1;const char *why="SERVICE_ORIGIN_UNCONFIRMED";char path[PATH_MAX],canonical[PATH_MAX],reply[16384],ready[65];
    if(argc!=6||!migration_path(argv[2])||!token(argv[3],96)||!hex64(argv[4])||!token(argv[5],32)||strlen(argv[5])!=32)return 64;
    memset(&sc,0,sizeof sc);sc.state=sc.guard=sc.op=sc.up=sc.cycles=sc.transition=-1;sc.may_publish=action==SC_START||action==SC_STOP||action==SC_RESTART;umask(077);
    if(!realpath(argv[2],canonical)||strcmp(canonical,argv[2])||migration_boot(boot))goto done;
    strcpy(sc.root,argv[2]);strcpy(sc.origin,argv[3]);strcpy(sc.migration,argv[4]);strcpy(sc.nonce,argv[5]);
    const char *prefix=strcmp(sc.root,"/")?sc.root:"";
    int n=snprintf(sc.statepath,sizeof sc.statepath,"%s/opt/var/lib/broray",prefix);if(n<0||n>=(int)sizeof sc.statepath)goto done;
    n=snprintf(sc.uppath,sizeof sc.uppath,"%s/opt/var/lib/broray-updater",prefix);if(n<0||n>=(int)sizeof sc.uppath)goto done;
    if(sc_join(path,sc.statepath,"operations")||sc_join(sc.oppath,path,sc.origin)||sc_join(sc.cyclepath,sc.uppath,"cycles"))goto done;
    sc.state=checked_directory(sc.statepath);if(sc.state<0)goto done;
    sc.guard=recovery_inherited_guard(sc.state);if(sc.guard<0)sc.guard=sc_lock_exact(sc.state,"operations.guard",0);
    if(sc.guard<0){why="SERVICE_TRANSITION_BUSY";goto done;}
    sc.op=checked_directory(sc.oppath);sc.up=checked_directory(sc.uppath);
    int self=open("/proc/self/exe",O_RDONLY|O_CLOEXEC);if(self<0)goto done;int bad=hash_fd(self,sc.native);close(self);if(bad||sc.op<0||sc.up<0)goto done;
    if(sc_join(path,sc.oppath,"platform-migration"))goto done;mig=checked_directory(path);
    if(mig<0||bg_source(mig,path,sc.root,sc.migration,&sc.input)||strcmp(sc.input.operation,sc.origin)||strcmp(sc.input.nonce,sc.nonce))goto done;
    char *verify[]={argv[0],"recovery-code-verify",sc.oppath,sc.root,path,sc.migration,NULL};
    if(recovery_code_impl(6,verify,0))goto done;
    int exists=bg_exists(sc.up,"cycles");if(exists<0)goto done;
    if(!exists){
        if(action!=SC_START||sc_pending_global()!=0||sc_create_seal(argv))goto done;fresh=1;
    }else{
        sc.cycles=checked_directory(sc.cyclepath);if(sc.cycles<0)goto done;
        int lock=bg_exists(sc.cycles,"transition.lock");if(lock<0)goto done;
        if(!lock){
            if(action!=SC_START||sc_pending_global()!=0||sc_origin_partial_names(sc.cycles)||sc_create_seal(argv))goto done;
            fresh=1;
        }else if(sc_seal_read())goto done;
    }
    sc.transition=sc_inherited_lock(sc.cycles,"transition.lock");
    if(sc.transition<0)sc.transition=sc_lock_exact(sc.cycles,"transition.lock",fresh);
    if(sc.transition<0){why="SERVICE_TRANSITION_BUSY";goto done;}
    why="SERVICE_CYCLE_HISTORY_UNCONFIRMED";if(sc_collect())goto done;
    struct sc_node *current=&sc.nodes[sc.current];
    if(action==SC_CURRENT){
        printf("{\"ok\":true,\"phase\":\"SERVICE_CURRENT_DISCOVERED\",\"generationId\":\"%s\",\"readinessProven\":false,\"activationAllowed\":false}\n",current->id);result=0;goto done;
    }
    int resume_restart=action==SC_RESTART?sc_restart_incomplete(current):0;
    if(resume_restart<0)goto done;
    if(action==SC_STOP||(action==SC_RESTART&&!resume_restart&&!strcmp(current->born,boot))){
        why="SERVICE_STOP_UNCONFIRMED";
        if(sc_call(argv,"recovery-service-stop",reply,sizeof reply)||!strstr(reply,"\"phase\":\"SERVICE_STOP_COMPLETED\"")||
           sc_stopped_seal(current,1))goto done;
        if(action==SC_STOP){fputs(reply,stdout);result=0;goto done;}
    }
    why="SERVICE_CYCLE_READY_UNCONFIRMED";
    if(action==SC_STATUS){if(sc_live(current)||sc_ready(current,0,ready))goto done;}
    else{
        if(sc_pending_global()!=0){why="SERVICE_TRANSACTION_PENDING";goto done;}
        if(!sc_live(current)){
            if(sc_ready(current,1,ready))goto done;
        }else{
            char retired[65],host[65];
            if(!sc_terminal(current,retired,host)){
                /* A stop-reply may have been lost before the ordinary-lifecycle
                 * seal. Settle only through the existing authenticated stop. */
                if(sc_stopped_seal(current,0)){
                    if(strcmp(current->born,boot)||sc_call(argv,"recovery-service-stop",reply,sizeof reply)||sc_stopped_seal(current,1))goto done;
                }
                if(sc_intent_publish(current)){why="SERVICE_START_INTENT_UNCONFIRMED";goto done;}current=&sc.nodes[sc.current];
            }else if(strcmp(current->born,boot)){
                why="SERVICE_BOOT_HISTORY_UNCONFIRMED";
                if(sc_boot_retire(current)||sc_boot_markers(current,1)||sc_intent_publish(current))goto done;
                current=&sc.nodes[sc.current];
            }else if(!current->index){why="SERVICE_GENERATION_UNCONFIRMED";goto done;}
            why="SERVICE_START_UNCONFIRMED";
            if(sc_start(current)||sc_ready(current,0,ready))goto done;
        }
    }
    /* Recheck provenance and exact live bytes/identities after publication.
     * A response is never the proof by itself; STATUS repeats these checks. */
    if(sc_tree_hash(sc.op,path)||strcmp(path,sc.tree)||sc_live(current)||sc_ready(current,0,ready))goto done;
    printf("{\"ok\":true,\"phase\":\"%s\",\"generationId\":\"%s\",\"commitReceiptSha256\":\"%s\",\"platformReady\":true,\"activationAllowed\":false}\n",fresh?"PREFLIGHT_COMPLETED":action==SC_STATUS?"COMMIT_VERIFIED":"SERVICE_READY",current->id,ready);result=0;
 done:if(mig>=0)close(mig);sc_close();return result?sc_fail(why):0;
}
/* The small transition flock bridges restart's STOP/START gap. It is NOT
 * the coordinator guard and grants no signalling authority. A new supervisor
 * keeps the authenticated inherited description only until its first ledger
 * and control socket are durable; the updater never inherits either guard. */
static int service_transition_prepare(int argc,char **argv){
    const char *text=getenv("BRORAY_SERVICE_TRANSITION_FD");if(!text)return 0;
    char *end=NULL;long fd=strtol(text,&end,10);
    if(!end||*end||fd<3||fd>INT_MAX||argc!=12||strcmp(argv[7],"platform-daemon"))return -1;
    service_transition_fd=fcntl((int)fd,F_DUPFD_CLOEXEC,3);
    return service_transition_fd<0||unsetenv("BRORAY_SERVICE_TRANSITION_FD")?-1:0;
}
static int service_transition_parent(void){
    struct identity before,after;char sha[65],path[64],bytes[16384];pid_t parent=getppid();
    if(parent<=1||capture(parent,&before)||peer_executable_hash(parent,sha)||strcmp(sha,pl.native))return -1;
    snprintf(path,sizeof path,"/proc/%d/cmdline",parent);ssize_t size=read_file(path,bytes,sizeof bytes-1);if(size<=0)return -1;
    char *parts[6];size_t at=0;unsigned count=0;
    while(at<(size_t)size&&count<6){char *end=memchr(bytes+at,0,(size_t)size-at);if(!end)return -1;parts[count++]=bytes+at;at=(size_t)(end-bytes)+1;}
    if(count!=6||at!=(size_t)size||
       (strcmp(parts[1],"recovery-resume")&&strcmp(parts[1],"service-cycle-start")&&strcmp(parts[1],"service-cycle-restart"))||
       strcmp(parts[2],pl.root)||strcmp(parts[3],pl.op)||!hex64(parts[4])||strcmp(parts[5],pl.nonce)||
       capture(parent,&after)||!identity_equal(&before,&after))return -1;
    return 0;
}
static int service_transition_enter(const char *domain){
    char parent[PATH_MAX],path[PATH_MAX];if(strlen(domain)>=sizeof parent)return -1;strcpy(parent,domain);
    char *end=strrchr(parent,'/');if(!end)return -1;*end=0;end=strrchr(parent,'/');
    if(!end||strcmp(end+1,"generations"))return service_transition_fd<0?0:-1;*end=0;
    if(sc_join(path,parent,"cycles"))return -1;struct stat st;
    if(lstat(path,&st)){return errno==ENOENT&&service_transition_fd<0?0:-1;}
    int directory=checked_directory(path);if(directory<0)return -1;
    struct stat named,held;int result=-1;
    if(fstatat(directory,"transition.lock",&named,AT_SYMLINK_NOFOLLOW)||!S_ISREG(named.st_mode)||named.st_uid!=geteuid()||named.st_nlink!=1||(named.st_mode&07777)!=0600||named.st_size)goto done;
    if(service_transition_fd>=0){
        if(!pl.enabled||service_transition_parent()||fstat(service_transition_fd,&held)||held.st_dev!=named.st_dev||held.st_ino!=named.st_ino||
           held.st_mode!=named.st_mode||held.st_uid!=named.st_uid||held.st_nlink!=1||held.st_size||
           (fcntl(service_transition_fd,F_GETFL)&O_ACCMODE)!=O_RDWR||flock(service_transition_fd,LOCK_EX|LOCK_NB))goto done;
    }else{
        service_transition_fd=openat(directory,"transition.lock",O_RDONLY|O_NOFOLLOW|O_CLOEXEC);
        if(service_transition_fd<0||fstat(service_transition_fd,&held)||held.st_dev!=named.st_dev||held.st_ino!=named.st_ino||
           held.st_mode!=named.st_mode||held.st_uid!=named.st_uid||held.st_nlink!=1||held.st_size||flock(service_transition_fd,LOCK_SH|LOCK_NB))goto done;
    }
    result=0;
 done:close(directory);return result;
}
static void service_transition_release(void){if(service_transition_fd>=0){close(service_transition_fd);service_transition_fd=-1;}}
