/* Ordinary S22 lifecycle, deliberately separate from migration retries.
 * The sealed, completed migration is read-only provenance. Every later launch
 * has its own durable intent under updater/cycles and uses the existing native
 * platform-daemon, lifetime exclusion and authenticated from-birth supervisor.
 */
#define SC_LIMIT 128
struct sc_node {char id[65],born[64],launch[65],transaction[65],host[65],chain[65];unsigned index;};
struct sc_context {
    int state,guard,op,up,cycles,transition,may_publish,replacement;
    char root[PATH_MAX],statepath[PATH_MAX],oppath[PATH_MAX],uppath[PATH_MAX],cyclepath[PATH_MAX];
    char origin[97],migration[65],nonce[65],native[65],tree[65],seal[65],initial[65];
    struct bg_input input;struct sc_node nodes[SC_LIMIT];unsigned count,baseline,current;
};
static struct sc_context sc;
/* Validated replacement attempts are historical members of the same origin.
 * They never become live service nodes merely by appearing in this list. */
static char rs_owned[SC_LIMIT][65];static unsigned rs_owned_count;
static int rs_attempts(struct sc_node *,const struct migration_file *,const struct migration_file *,const char *,const char *,int,int);
static int rs_birth(struct sc_node *,int);


static int rs_service_read(void);
static int rs_initial_boot_ready(char **argv,struct sc_node *n);
static int rs_prior_history(void);
static int rs_history_member(const char *family,const char *id);
static int sc_stopped_seal(struct sc_node *n,int publish);
static int sc_natural_record(struct sc_node *n,int publish,char nonce[65]);
static int sc_natural_retire(struct sc_node *n);
static void terminal_compact_installation(const char *up,const char *current);
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
    int unready;
};
/* Only the typed historical STOPPED repair may resume its own immutable
 * retirement publication. Expected bytes are derived again before publish. */
static int gb_stopped_retirement;
static int gb_text(const struct gb_record *b,char out[2048]){
    return snprintf(out,2048,"%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%lu\n%s\n%s\n",b->unready?"BROray-generation-unready-boot-ended/1":"BROray-generation-boot-ended/1",b->id,b->manifest,b->from,b->through,b->scope,b->seal,b->ready,b->host,b->journal,b->launch,b->transaction,b->total,b->inventory,b->last);
}
static int gb_parent(const char *domain,const char *id,char up[PATH_MAX]){
    if(strlen(domain)>=PATH_MAX)return -1;strcpy(up,domain);char *p=strrchr(up,'/');
    if(!p||strcmp(p+1,id))return -1;*p=0;p=strrchr(up,'/');
    if(!p||strcmp(p+1,"generations"))return -1;*p=0;return 0;
}
/* Boot evidence belongs to its sealed lifecycle origin. Replacement origins
 * have their own cycles-op-* directory; the legacy cycles directory is not
 * necessarily the origin of the generation being verified. Select by the
 * independently bound seal, never directory order, age or a live PID. */
static int gb_cycle_directory(const char *up,const char *seal){
    int parent=checked_directory(up),selected=-1,bad=0;DIR *d=NULL;
    if(parent<0)return -1;d=directory_stream(parent);if(!d){close(parent);return -1;}
    struct dirent *e;errno=0;
    while((e=readdir(d))){
        if(strcmp(e->d_name,"cycles")&&strncmp(e->d_name,"cycles-op-",10))continue;
        char path[PATH_MAX],anchor[128];struct migration_file record;memset(&record,0,sizeof record);
        if(sc_join(path,up,e->d_name)){bad=1;break;}
        int fd=checked_directory(path);
        if(fd<0||sc_file(fd,"origin.record",&record)){if(fd>=0)close(fd);free(record.bytes);bad=1;break;}
        if(!strcmp(record.sha,seal)){
            int n=snprintf(anchor,sizeof anchor,"BROray-service-origin-anchor/1\n%s\n",seal);
            if(selected>=0||n<0||n>=(int)sizeof anchor||bg_record_exact(fd,"origin.anchor",anchor,(size_t)n))bad=1;
            else{selected=fd;fd=-1;}
        }
        free(record.bytes);if(fd>=0)close(fd);if(bad)break;errno=0;
    }
    if(!e&&errno)bad=1;closedir(d);close(parent);
    if(bad&&selected>=0){close(selected);selected=-1;}return selected;
}
/* A later updater may verify an earlier boot's history. The sealed origin,
 * not the verifier's executable, pins that historical native runtime. Open
 * the retained bytes without symlinks and verify their exact private identity;
 * this is read-only provenance and grants no process/signal authority. */
static int gb_origin_native(const char *up,const struct migration_file *origin,const char *manifest_sha,char native[65]){
    char *copy=strdup(origin->bytes),*rows[7],*cursor=copy,path[PATH_MAX],sha[65];int root=-1,fd=-1,result=-1;
    if(!copy)return -1;
    for(unsigned i=0;i<7;i++){rows[i]=cursor;char *end=strchr(cursor,'\n');if(!end)goto done;*end=0;cursor=end+1;}
    if(strcmp(rows[0],"BROray-service-origin/1")||!migration_path(rows[1])||!token(rows[2],96)||
       !hex64(rows[3])||!token(rows[4],32)||strlen(rows[4])!=32||!hex64(rows[5])||strcmp(rows[6],manifest_sha))goto done;
    const char *prefix=strcmp(rows[1],"/")?rows[1]:"";
    int n=snprintf(path,sizeof path,"%s/opt/var/lib/broray-updater",prefix);
    if(n<0||n>=(int)sizeof path||strcmp(path,up))goto done;
    n=snprintf(path,sizeof path,"%s/runtimes/%s/runtime",up,rows[5]);
    if(n<0||n>=(int)sizeof path)goto done;
    root=migration_directory("/");if(root<0)goto done;fd=migration_relative(root,path+1);struct stat st;
    if(fd<0||fstat(fd,&st)||!S_ISREG(st.st_mode)||st.st_uid!=geteuid()||st.st_nlink!=1||
       (st.st_mode&07777)!=0700||hash_fd(fd,sha)||strcmp(sha,rows[5]))goto done;
    strcpy(native,rows[5]);result=0;
 done:if(fd>=0)close(fd);if(root>=0)close(root);free(copy);return result;
}
static int gb_measure_ledger(int base,const char *domain,struct gb_record *b,const struct migration_file *pinned,const char *native){
    char current[64],up[PATH_MAX],path[PATH_MAX],hostpath[PATH_MAX],name[128],prefix[512],sha[65],prior[65]="";
    struct migration_file ready,host;memset(&ready,0,sizeof ready);if(pinned)ready=*pinned;memset(&host,0,sizeof host);
    int h=-1,witness=-1,result=-1;DIR *dir=NULL;unsigned long maximum=0,found=0,anchor=0;
    if(migration_boot(current)||!token(b->id,64)||!hex64(b->manifest)||strlen(b->from)!=36||strlen(b->through)!=36||
       !token(b->from,36)||!token(b->through,36)||!strcmp(b->from,current)||!strcmp(b->from,b->through)||
       gb_parent(domain,b->id,up))goto done;
    scope_digest(domain,sha);if(strcmp(sha,b->scope))goto done;
    const char *accepted=NULL;int pn=0;
    if(pinned){
    int nn=snprintf(name,sizeof name,"ready-%s.record",b->id);
    pn=snprintf(prefix,sizeof prefix,"BROray-service-ready/1\n%s\n%s\n%s\n%s\n",b->seal,b->launch,b->transaction,b->host);
    if(nn<0||nn>=(int)sizeof name||pn<0||pn>=(int)sizeof prefix||!hex64(native)||
       strcmp(ready.sha,b->ready)||ready.size<=(size_t)pn||memcmp(ready.bytes,prefix,(size_t)pn))goto done;
    accepted=ready.bytes+pn;const char *rev=strstr(accepted,",\"revision\":");char field[256],observed[65];
    if(!rev||sscanf(rev,",\"revision\":%lu,",&anchor)!=1||!anchor||anchor>1000000||
       sc_field(accepted,"generationId",observed,sizeof observed)||strcmp(observed,b->id)||
       sc_field(accepted,"bootId",observed,sizeof observed)||strcmp(observed,b->from)||
       !strstr(accepted,"\"state\":\"RUNNING\",\"supervisedFromBirth\":true,")||!strstr(accepted,"\"platformReady\":true,"))goto done;
    snprintf(field,sizeof field,"\"nativeSha256\":\"%s\"",native);if(!strstr(accepted,field))goto done;
    }else{anchor=1;if(!hex64(native))goto done;}
    char observed[65];
    if(sc_join(path,up,"hosts")||sc_join(hostpath,path,b->id))goto done;
    h=checked_directory(hostpath);if(h<0||sc_file(h,"host.record",&host)||strcmp(host.sha,b->host)||service_journal_digest(h,b->journal))goto done;
    if(sc_field(host.bytes,"bootId",observed,sizeof observed)||strcmp(observed,b->from))goto done;
    dir=directory_stream(base);if(!dir)goto done;struct dirent *e;int bad=0;errno=0;
    while((e=readdir(dir))){const char *s=e->d_name;
        if(!strcmp(s,".")||!strcmp(s,".."))continue;
        if(gb_stopped_retirement&&(!strcmp(s,"retirement.receipt")||!strcmp(s,"retirement.receipt.pending"))){
            char pending[128];struct sc_record_pair pair;
            if(sc_record_names(base,"retirement.receipt",pending,&pair)||!pair.names){bad=1;break;}
            errno=0;continue;
        }
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
        if(!strcmp(s,"pending-boot-ended.receipt")||!strcmp(s,"pending-boot-ended.receipt.pending")){
            char pending[128];struct sc_record_pair pair;if(pinned||sc_record_names(base,"pending-boot-ended.receipt",pending,&pair)||!pair.names){bad=1;break;}
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
        /* Absence of a READY file is not proof of never having been ready.
         * Every independently witnessed revision must explicitly be unready;
         * removing a real READY pin cannot select this recovery contract. */
        if(b->unready&&(!strstr(bytes,"\"platformReady\":false,")||strstr(bytes,"\"platformReady\":true,")))bad=1;
        if(pinned&&nr==anchor&&(size!=ready.size-(size_t)pn||memcmp(bytes,accepted,size)))bad=1;
        if(!pinned&&nr==1){digest_bytes(bytes,size,sha);if(strcmp(sha,b->ready))bad=1;}
        if(!pinned&&nr==maximum){
            const char *keys[]={"nativeSha256","startIntentSha256","transactionRecordSha256","serviceHostRecordSha256"};
            const char *values[]={native,b->launch,b->transaction,b->host};
            for(unsigned j=0;j<4;j++){char field[192];int z=snprintf(field,sizeof field,"\"%s\":\"%s\"",keys[j],values[j]);if(z<0||z>=(int)sizeof field||!strstr(bytes,field))bad=1;}
        }
        if(!bad){
            digest_bytes(bytes,size,prior);char expected[384];
            int w=snprintf(expected,sizeof expected,"BROray-platform-ledger-witness/1\n%s\n%s\n%s\n%lu\n%s\n",b->id,b->manifest,b->from,nr,prior);
            if(w<0||w>=(int)sizeof expected||bg_record_exact(witness,name,expected,(size_t)w))bad=1;
            gen_sha_add(&inventory,name,strlen(name)+1);gen_sha_add(&inventory,prior,64);
        }
        free(bytes);if(bad)goto done;
    }
    b->total=maximum;strcpy(b->last,prior);gen_sha_end(&inventory,b->inventory);result=0;
 done:if(witness>=0)close(witness);if(dir)closedir(dir);if(h>=0)close(h);free(host.bytes);return result;
}
static int gb_measure(int base,const char *domain,struct gb_record *b){
    char up[PATH_MAX],name[128],native[65];int cycles=-1,start=-1,result=-1;
    struct migration_file ready,origin;memset(&ready,0,sizeof ready);memset(&origin,0,sizeof origin);
    struct migration_file launch,transaction;memset(&launch,0,sizeof launch);memset(&transaction,0,sizeof transaction);
    int n=snprintf(name,sizeof name,"ready-%s.record",b->id);
    if(n<0||n>=(int)sizeof name||gb_parent(domain,b->id,up))goto done;
    cycles=gb_cycle_directory(up,b->seal);
    if(cycles<0||sc_file(cycles,"origin.record",&origin)||strcmp(origin.sha,b->seal)||
       gb_origin_native(up,&origin,b->manifest,native))goto done;
    if(b->unready){
        char parent[PATH_MAX],path[PATH_MAX],binding[192];
        if(sc_record_exists(cycles,name)!=0||sc_file(base,"state.json",&ready)||strcmp(ready.sha,b->ready)||
           sc_join(parent,up,"starts")||sc_join(path,parent,b->id))goto done;
        start=checked_directory(path);
        int k=snprintf(binding,sizeof binding,"\n%s\n%s\n",b->seal,b->launch);
        if(start<0||sc_file(start,"launch.record",&launch)||strcmp(launch.sha,b->launch)||
           sc_file(start,"transaction.record",&transaction)||strcmp(transaction.sha,b->transaction)||
           strncmp(transaction.bytes,"BROray-service-start-intent/1\nSTART_INTENT\n",sizeof("BROray-service-start-intent/1\nSTART_INTENT\n")-1)||
           k<0||k>=(int)sizeof binding||!strstr(transaction.bytes,binding))goto done;
        result=gb_measure_ledger(base,domain,b,NULL,native);
    }else{
        if(sc_file(cycles,name,&ready))goto done;
        result=gb_measure_ledger(base,domain,b,&ready,native);
    }
 done:if(start>=0)close(start);if(cycles>=0)close(cycles);free(launch.bytes);free(transaction.bytes);free(ready.bytes);free(origin.bytes);return result;
}
static int gb_validate(int base,const char *domain,struct gb_record *out){
    struct migration_file record;memset(&record,0,sizeof record);struct gb_record b;memset(&b,0,sizeof b);
    char canonical[2048],magic[64],extra;int result=-1;
    if(sc_file(base,"boot-ended.receipt",&record))goto done;
    int count=sscanf(record.bytes,"%63s\n%64s\n%64s\n%63s\n%63s\n%64s\n%64s\n%64s\n%64s\n%64s\n%64s\n%64s\n%lu\n%64s\n%64s\n%c",magic,b.id,b.manifest,b.from,b.through,b.scope,b.seal,b.ready,b.host,b.journal,b.launch,b.transaction,&b.total,b.inventory,b.last,&extra);
    if(count!=15)goto done;
    if(!strcmp(magic,"BROray-generation-unready-boot-ended/1"))b.unready=1;
    else if(strcmp(magic,"BROray-generation-boot-ended/1"))goto done;
    int n=gb_text(&b,canonical);if(n<0||n>=2048||record.size!=(size_t)n||memcmp(record.bytes,canonical,(size_t)n)||
       !hex64(b.scope)||!hex64(b.seal)||!hex64(b.ready)||!hex64(b.host)||!hex64(b.journal)||!hex64(b.launch)||!hex64(b.transaction)||!hex64(b.inventory)||!hex64(b.last))goto done;
    struct gb_record actual=b;if(gb_measure(base,domain,&actual))goto done;
    n=gb_text(&actual,canonical);if(n<0||n>=2048||record.size!=(size_t)n||memcmp(record.bytes,canonical,(size_t)n))goto done;
    if(out)*out=b;result=0;
 done:free(record.bytes);return result;
}

/* An interrupted replacement has no READY pin. Its separately named receipt
 * proves only an ended boot plus the complete independently witnessed birth
 * ledger. It cannot authorize STOPPED, readiness, a signal or app activation. */
static int rs_pending_text(const struct gb_record *b,char out[2048]){
    return snprintf(out,2048,"BROray-generation-pending-boot-ended/1\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%lu\n%s\n%s\n",b->id,b->manifest,b->from,b->through,b->scope,b->seal,b->ready,b->host,b->journal,b->launch,b->transaction,b->total,b->inventory,b->last);
}
static int rs_pending_measure(int base,const char *domain,struct gb_record *b){
    char up[PATH_MAX],path[PATH_MAX],startpath[PATH_MAX],native[65],sha[65],*copy=NULL;int start=-1,runtime=-1,result=-1;
    struct migration_file launch,transaction,first;memset(&launch,0,sizeof launch);memset(&transaction,0,sizeof transaction);memset(&first,0,sizeof first);
    if(gb_parent(domain,b->id,up)||sc_join(path,up,"starts")||sc_join(startpath,path,b->id))goto done;
    start=checked_directory(startpath);
    if(start<0||sc_file(start,"launch.record",&launch)||strcmp(launch.sha,b->launch)||
       sc_file(start,"transaction.record",&transaction)||strcmp(transaction.sha,b->transaction)||sc_file(base,"state.json",&first)||strcmp(first.sha,b->ready))goto done;
    copy=strdup(launch.bytes);if(!copy)goto done;char *cursor=copy,*rows[10];
    for(unsigned i=0;i<10;i++){rows[i]=cursor;char *nl=strchr(cursor,'\n');if(!nl)goto done;*nl=0;cursor=nl+1;}
    if(strcmp(rows[0],"BROray-platform-launch/2")||!migration_path(rows[1])||strcmp(rows[2],domain)||strcmp(rows[3],b->id)||
       strcmp(rows[4],b->manifest)||!hex64(rows[5])||!migration_path(rows[6])||!hex64(rows[7])||!token(rows[8],96)||!token(rows[9],32)||strlen(rows[9])!=32)goto done;
    strcpy(native,rows[5]);digest_bytes(cursor,strlen(cursor),sha);if(strcmp(sha,b->manifest))goto done;
    const char *prefix=strcmp(rows[1],"/")?rows[1]:"";
    int k=snprintf(path,sizeof path,"%s/opt/var/lib/broray-updater",prefix);if(k<0||k>=(int)sizeof path||strcmp(path,up))goto done;
    char txprefix[160];k=snprintf(txprefix,sizeof txprefix,"BROray-platform-replacement-start-intent/1\nSTART_INTENT\n%s\n",b->seal);
    if(k<0||k>=(int)sizeof txprefix||transaction.size<=(size_t)k||memcmp(transaction.bytes,txprefix,(size_t)k))goto done;
    digest_bytes(transaction.bytes+k,transaction.size-(size_t)k,sha);if(strcmp(sha,b->seal))goto done;
    k=snprintf(path,sizeof path,"%s/runtimes/%s/runtime",up,native);if(k<0||k>=(int)sizeof path)goto done;
    int root=migration_directory("/");if(root<0)goto done;runtime=migration_relative(root,path+1);close(root);struct stat st;
    if(runtime<0||fstat(runtime,&st)||!S_ISREG(st.st_mode)||st.st_uid!=geteuid()||st.st_nlink!=1||(st.st_mode&07777)!=0700||hash_fd(runtime,sha)||strcmp(sha,native))goto done;
    result=gb_measure_ledger(base,domain,b,NULL,native);
done:if(start>=0)close(start);if(runtime>=0)close(runtime);free(copy);free(launch.bytes);free(transaction.bytes);free(first.bytes);return result;
}
static int rs_pending_validate(int base,const char *domain,struct gb_record *out){
    struct migration_file f;memset(&f,0,sizeof f);struct gb_record b;memset(&b,0,sizeof b);char text[2048],extra;int result=-1;
    if(sc_file(base,"pending-boot-ended.receipt",&f))goto done;
    int fields=sscanf(f.bytes,"BROray-generation-pending-boot-ended/1\n%64s\n%64s\n%63s\n%63s\n%64s\n%64s\n%64s\n%64s\n%64s\n%64s\n%64s\n%lu\n%64s\n%64s\n%c",b.id,b.manifest,b.from,b.through,b.scope,b.seal,b.ready,b.host,b.journal,b.launch,b.transaction,&b.total,b.inventory,b.last,&extra);
    int n=rs_pending_text(&b,text);
    if(fields!=14||n<0||n>=2048||f.size!=(size_t)n||memcmp(f.bytes,text,(size_t)n)||!hex64(b.seal)||!hex64(b.ready)||!hex64(b.host)||!hex64(b.launch)||!hex64(b.transaction)||!hex64(b.inventory)||!hex64(b.last))goto done;
    struct gb_record actual=b;if(rs_pending_measure(base,domain,&actual))goto done;n=rs_pending_text(&actual,text);
    if(n<0||n>=2048||f.size!=(size_t)n||memcmp(f.bytes,text,(size_t)n))goto done;
    if(out)*out=b;result=0;
done:free(f.bytes);return result;
}
static int generation_boot_retirement_valid(int base,const char *domain,const char *manifest_sha,const char *current){
    /* History may precede a platform upgrade. gb_validate binds EVERY record
     * and witness to that origin's own sealed manifest/native bytes. Requiring
     * the new platform's manifest here would reject valid older boot history,
     * unlike the already-supported ordinary retired-generation proof. */
    struct gb_record b;return !hex64(manifest_sha)||(gb_validate(base,domain,&b)&&rs_pending_validate(base,domain,&b))||(current&&!strcmp(current,b.id))?-1:0;
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
    size_t capacity=64;
    char (*names)[NAME_MAX+1]=calloc(capacity,sizeof *names);if(!names){closedir(d);return -1;}
    unsigned count=0;struct dirent *e;int bad=0;errno=0;
    while((e=readdir(d))){if(!strcmp(e->d_name,".")||!strcmp(e->d_name,".."))continue;
        if(*total==UINT_MAX||count==UINT_MAX){bad=1;break;}
        if(count==capacity){
            if(capacity>SIZE_MAX/2/sizeof *names){bad=1;break;}
            void *grown=realloc(names,capacity*2*sizeof *names);if(!grown){bad=1;break;}
            names=grown;capacity*=2;
        }
        ++*total;strcpy(names[count++],e->d_name);errno=0;}
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
static int sc_lock_exact_wait(int parent,const char *name,int create,unsigned wait_ms){
    int fd=openat(parent,name,O_RDWR|O_NOFOLLOW|O_CLOEXEC|(create?O_CREAT:0),0600);struct stat held,named;
    if(fd<0)return -1;
    unsigned long long until=millis()+wait_ms;
    for(;;){
    if(fstat(fd,&held)||!S_ISREG(held.st_mode)||held.st_uid!=geteuid()||held.st_nlink!=1||(held.st_mode&07777)!=0600||held.st_size||
       fstatat(parent,name,&named,AT_SYMLINK_NOFOLLOW)||held.st_dev!=named.st_dev||held.st_ino!=named.st_ino||held.st_mode!=named.st_mode||named.st_nlink!=1||named.st_uid!=held.st_uid||named.st_size){close(fd);errno=EINVAL;return -1;}
    if(!flock(fd,LOCK_EX|LOCK_NB))break;
    int error=errno;
    if(!wait_ms||(error!=EWOULDBLOCK&&error!=EINTR)){close(fd);errno=error;return -1;}
    if(millis()>=until){close(fd);errno=ETIMEDOUT;return -1;}
    struct timespec pause={0,10000000L};nanosleep(&pause,NULL);
    }
    /* The pathname must still name this same private empty inode after waiting.
     * Waiting proves only admission; the complete generation proof follows. */
    if(fstat(fd,&held)||!S_ISREG(held.st_mode)||held.st_uid!=geteuid()||held.st_nlink!=1||(held.st_mode&07777)!=0600||held.st_size||
       fstatat(parent,name,&named,AT_SYMLINK_NOFOLLOW)||held.st_dev!=named.st_dev||held.st_ino!=named.st_ino||held.st_mode!=named.st_mode||named.st_nlink!=1||named.st_uid!=held.st_uid||named.st_size||
       (create&&(fsync(fd)||fsync(parent)))){close(fd);errno=EINVAL;return -1;}return fd;
}
static int sc_lock_exact(int parent,const char *name,int create){return sc_lock_exact_wait(parent,name,create,0);}
static int sc_call(char **argv,const char *verb,char *reply,size_t capacity){
    if(sc.replacement){
        if(!strcmp(verb,"recovery-commit-check"))verb="replacement-origin-proof";
        else if(!strcmp(verb,"recovery-service-stop"))verb="replacement-service-stop-exec";
    }
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
    /* Initial proof may be live or an exact committed generation from an
     * ended kernel boot. The latter explicitly grants no live readiness. */
    if(sc_call(argv,"recovery-commit-check",reply,sizeof reply)||
       !((strstr(reply,"\"phase\":\"COMMIT_VERIFIED\"")&&strstr(reply,"\"platformReady\":true"))||
         (sc.replacement&&(strstr(reply,"\"phase\":\"COMMITTED_BOOT_ENDED\"")||strstr(reply,"\"phase\":\"COMMITTED_STOPPED\""))&&strstr(reply,"\"platformReady\":false")))||
       sc_field(reply,"generationId",sc.initial,sizeof sc.initial)||
       sc_tree_hash(sc.op,sc.tree)||sc_join(parent,sc.uppath,"starts"))goto done;
    starts=checked_directory(parent);if(starts<0)goto done;DIR *dir=directory_stream(starts);if(!dir)goto done;
    struct dirent *e;int bad=0;errno=0;
    while((e=readdir(dir))){
        if(!strcmp(e->d_name,".")||!strcmp(e->d_name,".."))continue;
        if(sc.replacement&&strcmp(e->d_name,sc.initial))continue;
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
        const char *name=strrchr(sc.cyclepath,'/');if(!name||mkdirat(sc.up,name+1,0700)||fsync(sc.up))goto done;
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
    /* Ordinary service stops have their own operation. Natural retirement of
     * the committed initial node or a successor binds to this origin and its
     * immutable intent. Absence of that intent cannot become valid history. */
    if(!strcmp(r.op,sc.origin)&&(n->index||!strcmp(n->id,sc.initial))){
        char nonce[65];if(sc_natural_record(n,0,nonce)||strcmp(r.nonce,nonce))goto done;
    }
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
       sc_file(sc.op,sc.replacement?"platform-replacement-target/manifest.record":"platform-migration/manifest.record",&manifest)||strcmp(manifest.sha,sc.input.manifest))goto done;
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
        if(at<0&&sc.replacement&&rs_history_member(family,e->d_name)==0){
            if(fstatat(fd,e->d_name,&st,AT_SYMLINK_NOFOLLOW)||!S_ISDIR(st.st_mode)||st.st_uid!=geteuid()||(st.st_mode&07777)!=0700){bad=1;break;}
            errno=0;continue;
        }
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
        if(!allowed){
            for(unsigned i=0;i<sc.count;i++){
                snprintf(want,sizeof want,"natural-stop-%s.record",sc.nodes[i].id);
                if(!strcmp(name,want)){
                    char nonce[65];if(sc_natural_record(&sc.nodes[i],0,nonce)){bad=1;break;}
                    allowed=1;break;
                }
            }
            if(bad)break;
        }
        if(!allowed){
            for(unsigned i=0;i<sc.count;i++){
                snprintf(want,sizeof want,"boot-residue-%s.record",sc.nodes[i].id);
                if(!strcmp(name,want)){
                    struct gb_record ended;char proof[65];
                    if(sc_boot_proof(&sc.nodes[i],&ended,proof)){bad=1;break;}
                    allowed=1;break;
                }
            }
            if(bad)break;
        }
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
        free(f.bytes);free(copy);free(record);free(launch);free(transaction);if(bad){fprintf(stderr,"SERVICE_HISTORY_ERROR=cycle-%u\n",index);return -1;}
    }
    if(sc.count>=SC_LIMIT)return -1;
    if(sc.replacement&&rs_prior_history()){fprintf(stderr,"SERVICE_HISTORY_ERROR=prior-history\n");return -1;}
    if(sc_cycle_names(index-1)){fprintf(stderr,"SERVICE_HISTORY_ERROR=cycle-names\n");return -1;}
    const char *families[]={"starts","generations","hosts"};
    for(unsigned i=0;i<3;i++)if(sc_namespace(families[i],1)){fprintf(stderr,"SERVICE_HISTORY_ERROR=namespace-%s\n",families[i]);return -1;}
    for(unsigned i=0;i<sc.count;i++)if(i!=sc.current){char a[65],b[65];struct gb_record ended;if(sc_terminal(&sc.nodes[i],a,b)&&(sc_boot_proof(&sc.nodes[i],&ended,a)||sc_boot_markers(&sc.nodes[i],0))){fprintf(stderr,"SERVICE_HISTORY_ERROR=predecessor-%u\n",i);return -1;}}
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
    if(!strcmp(n->born,boot)||sc_load_node(n))return -1;
    int domain=checked_directory(pl.domain),host=checked_directory(pl.host),gens=-1,whole=-1,life=-1,result=-1;
    char path[PATH_MAX],name[128],text[2048],current[64];struct gb_record b;memset(&b,0,sizeof b);
    struct migration_file r,saved;memset(&r,0,sizeof r);memset(&saved,0,sizeof saved);
    struct migration_file host_record;memset(&host_record,0,sizeof host_record);
    if(domain<0||host<0||sc_join(path,sc.uppath,"generations"))goto done;gens=checked_directory(path);
    if(gens<0||(whole=sc_lock_exact(gens,".generation-lifetime.lock",0))<0||
       (life=sc_lock_exact(domain,"lifetime.lock",0))<0||flock(host,LOCK_EX|LOCK_NB)||migration_boot(current)||!strcmp(current,n->born))goto done;
    /* An ordinary failed start has no READY pin for its host. Bind the exact
     * private host record to the complete witnessed ledger before publishing
     * any boot proof. This grants no live process or signalling authority. */
    if(sc_file(host,"host.record",&host_record)||(n->host[0]&&strcmp(n->host,host_record.sha)))goto done;
    strcpy(n->host,host_record.sha);
    strcpy(b.id,n->id);strcpy(b.manifest,sc.input.manifest);strcpy(b.from,n->born);strcpy(b.through,current);
    strcpy(b.seal,sc.seal);strcpy(b.host,n->host);strcpy(b.launch,n->launch);strcpy(b.transaction,n->transaction);scope_digest(pl.domain,b.scope);
    int k=snprintf(name,sizeof name,"ready-%s.record",n->id);
    if(k<0||k>=(int)sizeof name)goto done;
    int ready_exists=sc_record_exists(sc.cycles,name);if(ready_exists<0)goto done;
    if(ready_exists){if(sc_file(sc.cycles,name,&r))goto done;}
    else{
        if(!n->index||sc_file(domain,"state.json",&r))goto done;
        b.unready=1;
    }
    strcpy(b.ready,r.sha);
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
 done:if(life>=0)close(life);if(whole>=0)close(whole);if(gens>=0)close(gens);if(host>=0)close(host);if(domain>=0)close(domain);free(host_record.bytes);free(r.bytes);free(saved.bytes);return result;
}
/* Preserve old daemon projections under their own boot receipt instead of
 * deleting them. Only exact, private, boot-ended markers may move. A foreign
 * projection, symlink, changed inode or unknown residue fences the transition.
 */
static int sc_boot_markers(struct sc_node *n,int finish){
    struct gb_record b;char proof[65],name[128],path[PATH_MAX],expected[32];struct migration_file r;memset(&r,0,sizeof r);
    struct migration_file saved;memset(&saved,0,sizeof saved);
    char receipt_name[128],receipt[256];unsigned mask=7;
    int archive=-1,result=-1;if(sc_boot_proof(n,&b,proof))goto done;
    int k;
    if(b.unready){
        int domain=checked_directory(pl.domain);if(domain<0)goto done;record_name(b.total,name);
        int bad=sc_file(domain,name,&r);close(domain);if(bad||strcmp(r.sha,b.last))goto done;
    }else{
        k=snprintf(name,sizeof name,"ready-%s.record",n->id);if(k<0||k>=(int)sizeof name||sc_file(sc.cycles,name,&r))goto done;
    }
    const char *u=strstr(r.bytes,",\"updater\":{\"pid\":");long pid;
    if(!u||sscanf(u,",\"updater\":{\"pid\":%ld,",&pid)!=1||pid<=1||pid>INT_MAX)goto done;
    k=snprintf(expected,sizeof expected,"%ld\n",pid);if(k<=0||k>=(int)sizeof expected)goto done;
    int nn=snprintf(name,sizeof name,"boot-residue-%s",n->id);if(nn<0||nn>=(int)sizeof name||sc_join(path,sc.cyclepath,name))goto done;
    archive=sc_directory(sc.cycles,name,path,finish);if(archive<0)goto done;
    DIR *d=directory_stream(archive);if(!d)goto done;struct dirent *e;int bad=0;errno=0;
    while((e=readdir(d))){if(strcmp(e->d_name,".")&&strcmp(e->d_name,"..")&&strcmp(e->d_name,"daemon.pid")&&strcmp(e->d_name,"daemon.ready")&&strcmp(e->d_name,"daemon.lock")){bad=1;break;}errno=0;}
    if(!e&&errno)bad=1;closedir(d);if(bad)goto done;
    const char *files[]={"daemon.pid","daemon.ready","daemon.lock"};
    int rn=snprintf(receipt_name,sizeof receipt_name,"boot-residue-%s.record",n->id);
    if(rn<0||rn>=(int)sizeof receipt_name)goto done;
    int has_receipt=sc_record_exists(sc.cycles,receipt_name);if(has_receipt<0)goto done;
    if(has_receipt){
        char pinned[65],extra;long saved_pid;
        if(sc_service_file(sc.cycles,receipt_name,&saved,NULL)||
           sscanf(saved.bytes,"BROray-boot-residue/1\n%64s\n%ld\n%u\n%c",pinned,&saved_pid,&mask,&extra)!=3||
           strcmp(pinned,proof)||saved_pid!=pid||mask>7)goto done;
    }else if(finish){
        mask=0;
        for(unsigned i=0;i<3;i++){
            int a=bg_exists(sc.up,files[i]),z=bg_exists(archive,files[i]);
            if(a<0||z<0||a+z>1)goto done;if(a+z)mask|=1U<<i;
        }
    }
    /* With no new presence receipt, historical completed archives retain the
     * original all-three contract. New archives durably distinguish absent
     * projections from incomplete moves. The boot proof, never the PID
     * projection, establishes that the old processes cannot survive. */
    rn=snprintf(receipt,sizeof receipt,"BROray-boot-residue/1\n%s\n%ld\n%u\n",proof,pid,mask);
    if(rn<0||rn>=(int)sizeof receipt||(has_receipt&&(saved.size!=(size_t)rn||memcmp(saved.bytes,receipt,(size_t)rn))))goto done;
    for(unsigned i=0;i<3;i++){
        int a=finish?bg_exists(sc.up,files[i]):0,z=bg_exists(archive,files[i]);
        if(a<0||z<0||a+z!=!!(mask&(1U<<i)))goto done;
        if(!(mask&(1U<<i)))continue;int dir=a?sc.up:archive;
        if(i<2){if(bg_record_exact(dir,files[i],expected,(size_t)k))goto done;}
        else{int lock=openat(dir,files[i],O_RDONLY|O_DIRECTORY|O_NOFOLLOW|O_CLOEXEC);struct stat st;
            int invalid=lock<0;if(!invalid)invalid=fstat(lock,&st)||st.st_uid!=geteuid()||(st.st_mode&07777)!=0700||bg_empty(lock);
            if(lock>=0)close(lock);if(invalid)goto done;}
    }
    if((finish||has_receipt)&&sc_publish(sc.cycles,receipt_name,receipt,(size_t)rn,finish))goto done;
    for(unsigned i=0;i<3;i++){
        int a=finish?bg_exists(sc.up,files[i]):0,z=bg_exists(archive,files[i]);if(a<0||z<0||a+z!=!!(mask&(1U<<i)))goto done;
        if(!(mask&(1U<<i)))continue;
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
 done:if(archive>=0)close(archive);free(r.bytes);free(saved.bytes);return result;
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
/* A natural daemon exit has already drained its entire writer domain. Preserve
 * that exact witnessed checkpoint BEFORE binding a retirement request. This is
 * lifecycle evidence, not a new stop authorization for a running process. */
static int sc_natural_record(struct sc_node *n,int publish,char nonce[65]){
    char name[128],prefix[768],ready[65],record[64],id[65],born[64],digest[65];
    char *text=NULL;size_t size=0;struct migration_file saved={0};int domain=-1,result=-1;
    int nn=snprintf(name,sizeof name,"natural-stop-%s.record",n->id);
    int pn=snprintf(prefix,sizeof prefix,"BROray-service-natural-stop/1\n%s\n%s\n%s\n%s\n%s\n%s\n",sc.seal,n->id,n->born,n->launch,n->transaction,n->host);
    if(nn<0||nn>=(int)sizeof name||pn<0||pn>=(int)sizeof prefix||sc_load_node(n)||sc_host_record(n))goto done;
    int present=sc_record_exists(sc.cycles,name);if(present<0||(!present&&!publish))goto done;
    if(!present){
        if(strcmp(n->born,boot)||!strstr(snapshot,"\"state\":\"STOPPED\"")||
           !strstr(snapshot,"\"stopOperationId\":\"\",\"stopNonce\":\"\"")||
           !strstr(snapshot,"\"children\":[],\"awaitingBirth\":[],\"exitedUnreaped\":[]")||
           !strstr(snapshot,"\"platformReady\":false")||sc_ready(n,0,ready))goto done;
        FILE *f=open_memstream(&text,&size);if(!f)goto done;
        fputs(prefix,f);fputs(snapshot,f);if(fclose(f)||sc_publish(sc.cycles,name,text,size,1))goto done;
    }
    if(sc_service_file(sc.cycles,name,&saved,NULL)||saved.size<=(size_t)pn||memcmp(saved.bytes,prefix,(size_t)pn))goto done;
    const char *checkpoint=saved.bytes+pn;unsigned long rev=0;const char *r=strstr(checkpoint,",\"revision\":");
    if(!r||sscanf(r,",\"revision\":%lu,",&rev)!=1||!rev||rev>1000000||
       sc_field(checkpoint,"generationId",id,sizeof id)||strcmp(id,n->id)||
       sc_field(checkpoint,"bootId",born,sizeof born)||strcmp(born,n->born)||
       !strstr(checkpoint,"\"state\":\"STOPPED\"")||!strstr(checkpoint,"\"platformReady\":false")||
       !strstr(checkpoint,"\"stopOperationId\":\"\",\"stopNonce\":\"\"")||
       !strstr(checkpoint,"\"children\":[],\"awaitingBirth\":[],\"exitedUnreaped\":[]"))goto done;
    domain=checked_directory(pl.domain);record_name(rev,record);
    if(domain<0||bg_record_exact(domain,record,checkpoint,saved.size-(size_t)pn)||sc_publish(sc.cycles,name,saved.bytes,saved.size,0))goto done;
    digest_bytes(saved.bytes,saved.size,digest);memcpy(nonce,digest,32);nonce[32]=0;result=0;
done:if(domain>=0)close(domain);free(text);free(saved.bytes);return result;
}
static int sc_natural_retire(struct sc_node *n){
    char nonce[65],retired[65],host[65],ready[65],field[192];
    if(strcmp(n->born,boot)||sc_load_node(n)||sc_host_record(n)||pl_exact())return -1;
    if(!sc_terminal(n,retired,host)){
        if(sc_natural_record(n,0,nonce))return -1;
    }else{
        char *status[]={pl.nativepath,"control",pl.domain,"STATUS",n->id,sc.input.manifest,sc.origin,sc.nonce,NULL};
        if(control_exchange(8,status,0)||!strstr(snapshot,"\"state\":\"STOPPED\"")||
           !strstr(snapshot,"\"children\":[],\"awaitingBirth\":[],\"exitedUnreaped\":[]")||
           !strstr(snapshot,"\"platformReady\":false")||sc_ready(n,0,ready)||sc_natural_record(n,1,nonce))return -1;
        int k=snprintf(field,sizeof field,"\"stopOperationId\":\"%s\",\"stopNonce\":\"%s\"",sc.origin,nonce);
        if(k<0||k>=(int)sizeof field||(!strstr(snapshot,"\"stopOperationId\":\"\",\"stopNonce\":\"\"")&&!strstr(snapshot,field)))return -1;
        char *stop[]={pl.nativepath,"control",pl.domain,"STOP",n->id,sc.input.manifest,sc.origin,nonce,NULL};
        /* STOP on authenticated terminal state only records this durable
         * request. The supervisor's existing terminal branch sends no signal. */
        if(control_exchange(8,stop,0)||!strstr(snapshot,"\"state\":\"STOPPED\"")||!strstr(snapshot,field))return -1;
        stop[3]="RETIRE";if(control_exchange(8,stop,0))return -1;
    }
    unsigned long long until=millis()+2000;
    while(sc_terminal(n,retired,host)){
        if(millis()>=until)return -1;struct timespec pause={0,20000000L};nanosleep(&pause,NULL);
    }
    int k=snprintf(field,sizeof field,"\"stopOperationId\":\"%s\",\"stopNonce\":\"%s\"",sc.origin,nonce);
    return k<0||k>=(int)sizeof field||!strstr(snapshot,field)||sc_stopped_seal(n,1)||pl_exact()?-1:0;
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
    terminal_compact_installation(sc.uppath,n->id);
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
    sc.replacement=!strncmp(argv[1],"replacement-service-",20);
    if(!realpath(argv[2],canonical)||strcmp(canonical,argv[2])||migration_boot(boot))goto done;
    strcpy(sc.root,argv[2]);strcpy(sc.origin,argv[3]);strcpy(sc.migration,argv[4]);strcpy(sc.nonce,argv[5]);
    const char *prefix=strcmp(sc.root,"/")?sc.root:"";
    int n=snprintf(sc.statepath,sizeof sc.statepath,"%s/opt/var/lib/broray",prefix);if(n<0||n>=(int)sizeof sc.statepath)goto done;
    n=snprintf(sc.uppath,sizeof sc.uppath,"%s/opt/var/lib/broray-updater",prefix);if(n<0||n>=(int)sizeof sc.uppath)goto done;
    if(sc_join(path,sc.statepath,"operations")||sc_join(sc.oppath,path,sc.origin)||sc_join(sc.cyclepath,sc.uppath,"cycles"))goto done;
    char cycle_name[128]="cycles";
    if(sc.replacement){
        n=snprintf(cycle_name,sizeof cycle_name,"cycles-%s",sc.origin);
        if(n<0||n>=(int)sizeof cycle_name||sc_join(sc.cyclepath,sc.uppath,cycle_name))goto done;
    }
    sc.state=checked_directory(sc.statepath);if(sc.state<0)goto done;
    sc.guard=recovery_inherited_guard(sc.state);if(sc.guard<0)sc.guard=sc_lock_exact_wait(sc.state,"operations.guard",0,action==SC_STATUS?30000:0);
    if(sc.guard<0){why="SERVICE_TRANSITION_BUSY";goto done;}
    sc.op=checked_directory(sc.oppath);sc.up=checked_directory(sc.uppath);
    int self=open("/proc/self/exe",O_RDONLY|O_CLOEXEC);if(self<0)goto done;int bad=hash_fd(self,sc.native);close(self);if(bad||sc.op<0||sc.up<0)goto done;
    if(sc.replacement){if(rs_service_read())goto done;}
    else{
    if(sc_join(path,sc.oppath,"platform-migration"))goto done;mig=checked_directory(path);
    if(mig<0||bg_source(mig,path,sc.root,sc.migration,&sc.input)||strcmp(sc.input.operation,sc.origin)||strcmp(sc.input.nonce,sc.nonce))goto done;
    char *verify[]={argv[0],"recovery-code-verify",sc.oppath,sc.root,path,sc.migration,NULL};
    if(recovery_code_impl(6,verify,0))goto done;
    }
    int exists=bg_exists(sc.up,cycle_name);if(exists<0)goto done;
    if(!exists){
        /* A completed preflight can already own a live, ready generation
         * before the first ordinary init start. STOP must authenticate and
         * seal that same origin before entering its existing protected stop;
         * it must not require a redundant start or launch another process. */
        if((action!=SC_START&&action!=SC_STOP)||sc_pending_global()!=0||sc_create_seal(argv))goto done;fresh=1;
    }else{
        sc.cycles=checked_directory(sc.cyclepath);if(sc.cycles<0)goto done;
        int lock=bg_exists(sc.cycles,"transition.lock");if(lock<0)goto done;
        if(!lock){
            if((action!=SC_START&&action!=SC_STOP)||sc_pending_global()!=0||sc_origin_partial_names(sc.cycles)||sc_create_seal(argv))goto done;
            fresh=1;
        }else if(sc_seal_read())goto done;
    }
    sc.transition=sc_inherited_lock(sc.cycles,"transition.lock");
    if(sc.transition<0)sc.transition=sc_lock_exact(sc.cycles,"transition.lock",fresh);
    if(sc.transition<0){why="SERVICE_TRANSITION_BUSY";goto done;}
    why="SERVICE_CYCLE_HISTORY_UNCONFIRMED";
    if(sc.replacement&&sc.count==sc.baseline&&!strcmp(sc.nodes[sc.current].id,sc.initial)&&
       (strcmp(sc.nodes[sc.current].born,boot)||sc_live(&sc.nodes[sc.current]))&&rs_initial_boot_ready(argv,&sc.nodes[sc.current]))goto done;
    if(sc_collect())goto done;
    struct sc_node *current=&sc.nodes[sc.current];
    if(action==SC_CURRENT){
        printf("{\"ok\":true,\"phase\":\"SERVICE_CURRENT_DISCOVERED\",\"generationId\":\"%s\",\"readinessProven\":false,\"activationAllowed\":false}\n",current->id);result=0;goto done;
    }
    int resume_restart=action==SC_RESTART?sc_restart_incomplete(current):0;
    if(resume_restart<0)goto done;
    if(action==SC_STOP||(action==SC_RESTART&&!resume_restart&&!strcmp(current->born,boot))){
        why="SERVICE_STOP_UNCONFIRMED";
        /* The stop coordinator rechecks this lifecycle through read-only
         * status. Publish the same authenticated READY evidence as start
         * before entering it, including the initial preflight generation.
         * A completed stop still follows its existing terminal replay. */
        if(!sc_live(current)&&sc_ready(current,1,ready))goto done;
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
            /* A natural root exit needs terminal bookkeeping, not a signal to
             * a presumed PID. An existing managed STOP keeps its original path. */
            char natural_name[128];int nk=snprintf(natural_name,sizeof natural_name,"natural-stop-%s.record",current->id);
            if(nk<0||nk>=(int)sizeof natural_name)goto done;
            int natural=sc_record_exists(sc.cycles,natural_name);if(natural<0)goto done;
            int ended=0;
            if(!natural&&!strcmp(current->born,boot)&&!sc_load_node(current)){
                char *status[]={pl.nativepath,"control",pl.domain,"STATUS",current->id,sc.input.manifest,sc.origin,sc.nonce,NULL};
                ended=!control_exchange(8,status,0)&&strstr(snapshot,"\"state\":\"STOPPED\"")&&
                    strstr(snapshot,"\"stopOperationId\":\"\",\"stopNonce\":\"\"");
            }
            if(!strcmp(current->born,boot)&&(natural||ended)){
                why="SERVICE_TERMINAL_EXIT_UNCONFIRMED";
                if(sc_natural_retire(current))goto done;
            }
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
static int service_transition_parent(char directory[PATH_MAX]){
    struct identity before,after;char sha[65],path[64],bytes[16384];pid_t parent=getppid();
    if(parent<=1||capture(parent,&before)||peer_executable_hash(parent,sha)||strcmp(sha,pl.native))return -1;
    snprintf(path,sizeof path,"/proc/%d/cmdline",parent);ssize_t size=read_file(path,bytes,sizeof bytes-1);if(size<=0)return -1;
    char *parts[7];size_t at=0;unsigned count=0;
    while(at<(size_t)size&&count<7){char *end=memchr(bytes+at,0,(size_t)size-at);if(!end)return -1;parts[count++]=bytes+at;at=(size_t)(end-bytes)+1;}
    if(at!=(size_t)size||count<6||strcmp(parts[2],pl.root)||strcmp(parts[3],pl.op))return -1;
    const char *prefix=strcmp(pl.root,"/")?pl.root:"";int n;
    if(count==7&&!strcmp(parts[1],"replacement-start")){
        /* This parent has authenticated the protected replacement intent.
         * Bind its exact state/fence and PRIVATE transition inode; neither an
         * environment descriptor nor the old origin's lock authorizes B. */
        if(strcmp(parts[4],pl.nonce)||strcmp(parts[5],pl.manifest)||!hex64(parts[6]))return -1;
        char op_path[PATH_MAX],fence[PATH_MAX],expected[PATH_MAX],link[PATH_MAX];
        n=snprintf(op_path,sizeof op_path,"%s/opt/var/lib/broray/operations/%s",prefix,pl.op);
        if(n<0||n>=(int)sizeof op_path)return -1;
        int op=checked_directory(op_path);struct migration_file state;memset(&state,0,sizeof state);
        int bad=op<0||sc_file(op,"state.json",&state)||strcmp(state.sha,parts[6]);
        if(op>=0)close(op);free(state.bytes);if(bad)return -1;
        n=snprintf(fence,sizeof fence,"%s/opt/var/lock/broray/global-operation.lock",prefix);
        if(n<0||n>=(int)sizeof fence||sc_join(expected,op_path,"fence"))return -1;
        ssize_t got=readlink(fence,link,sizeof link);
        if(got!=(ssize_t)strlen(expected)||memcmp(link,expected,(size_t)got)||
           sc_join(directory,op_path,"platform-replacement-start"))return -1;
    }else{
        int replacement=count==6&&(!strcmp(parts[1],"replacement-service-start")||!strcmp(parts[1],"replacement-service-restart"));
        if(count!=6||(!replacement&&strcmp(parts[1],"recovery-resume")&&strcmp(parts[1],"service-cycle-start")&&strcmp(parts[1],"service-cycle-restart"))||
           !hex64(parts[4])||strcmp(parts[5],pl.nonce))return -1;
        n=replacement?snprintf(directory,PATH_MAX,"%s/opt/var/lib/broray-updater/cycles-%s",prefix,pl.op):
                      snprintf(directory,PATH_MAX,"%s/opt/var/lib/broray-updater/cycles",prefix);
        if(n<0||n>=PATH_MAX)return -1;
    }
    if(capture(parent,&after)||!identity_equal(&before,&after))return -1;
    return 0;
}
static int service_transition_enter(const char *domain){
    char parent[PATH_MAX],path[PATH_MAX];if(strlen(domain)>=sizeof parent)return -1;strcpy(parent,domain);
    char *end=strrchr(parent,'/');if(!end)return -1;*end=0;end=strrchr(parent,'/');
    if(!end||strcmp(end+1,"generations"))return service_transition_fd<0?0:-1;*end=0;
    if(service_transition_fd>=0){if(!pl.enabled||service_transition_parent(path))return -1;}
    else if(sc_join(path,parent,"cycles"))return -1;struct stat st;
    if(lstat(path,&st)){return errno==ENOENT&&service_transition_fd<0?0:-1;}
    int directory=checked_directory(path);if(directory<0)return -1;
    struct stat named,held;int result=-1;
    if(fstatat(directory,"transition.lock",&named,AT_SYMLINK_NOFOLLOW)||!S_ISREG(named.st_mode)||named.st_uid!=geteuid()||named.st_nlink!=1||(named.st_mode&07777)!=0600||named.st_size)goto done;
    if(service_transition_fd>=0){
        if(fstat(service_transition_fd,&held)||held.st_dev!=named.st_dev||held.st_ino!=named.st_ino||
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

/* Replacement start uses the existing from-birth launcher. Its transaction is
 * distinct from legacy boot migration and never claims an old boot ended. */
static int rs_context(const char *root,const char *operation,const char *nonce){
    char path[PATH_MAX];memset(&sc,0,sizeof sc);sc.state=sc.guard=sc.op=sc.up=sc.cycles=sc.transition=-1;sc.may_publish=1;
    if(!migration_path(root)||!token(operation,96)||!token(nonce,32)||strlen(nonce)!=32||migration_boot(boot))return -1;
    rs_owned_count=0;strcpy(sc.root,root);strcpy(sc.origin,operation);strcpy(sc.nonce,nonce);
    const char *prefix=strcmp(root,"/")?root:"";
    if(snprintf(sc.statepath,sizeof sc.statepath,"%s/opt/var/lib/broray",prefix)>=(int)sizeof sc.statepath||
       snprintf(sc.uppath,sizeof sc.uppath,"%s/opt/var/lib/broray-updater",prefix)>=(int)sizeof sc.uppath||
       sc_join(path,sc.statepath,"operations")||sc_join(sc.oppath,path,operation)||
       sc_join(sc.cyclepath,sc.oppath,"platform-replacement-start"))return -1;
    sc.state=checked_directory(sc.statepath);sc.op=checked_directory(sc.oppath);sc.up=checked_directory(sc.uppath);
    if(sc.state<0||sc.op<0||sc.up<0||(sc.guard=recovery_inherited_guard(sc.state))<0||peer_executable_hash(getpid(),sc.native))return -1;
    return 0;
}
static int rs_fence_check(const char *state_sha,int completed){
    char path[PATH_MAX],wanted[PATH_MAX],link[PATH_MAX];struct migration_file state;memset(&state,0,sizeof state);
    const char *prefix=strcmp(sc.root,"/")?sc.root:"";
    if(snprintf(path,sizeof path,"%s/opt/var/lock/broray/global-operation.lock",prefix)>=(int)sizeof path||sc_join(wanted,sc.oppath,"fence"))return -1;
    ssize_t n=readlink(path,link,sizeof link);
    if(n!=(ssize_t)strlen(wanted)||memcmp(link,wanted,(size_t)n)){
        if(!completed||sc_join(path,sc.oppath,"retired-lock"))return -1;
        n=readlink(path,link,sizeof link);
        if(n!=(ssize_t)strlen(wanted)||memcmp(link,wanted,(size_t)n))return -1;
    }
    if(sc_file(sc.op,"state.json",&state))return -1;
    int bad=strcmp(state.sha,state_sha);free(state.bytes);return bad?-1:0;
}
static int rs_fence(const char *state_sha){return rs_fence_check(state_sha,0);}
/* Terminal generation/host directories retain their control socket as
 * evidence. This inventory is used only after full retirement proof; it never
 * connects to that socket or interprets its presence as a live owner. The
 * ordinary operation-tree verifier continues to reject all sockets. */
static int rs_history_tree_raw(int fd,const char *root,char sha[65],int omit_summary){
    DIR *d=directory_stream(fd);if(!d)return -1;
    size_t capacity=64;
    char (*names)[NAME_MAX+1]=calloc(capacity,sizeof *names);if(!names){closedir(d);return -1;}
    unsigned count=0;int bad=0;struct dirent *e;errno=0;
    while((e=readdir(d))){
        if(!strcmp(e->d_name,".")||!strcmp(e->d_name,".."))continue;
        if(omit_summary&&(!strcmp(e->d_name,"retention.record")||!strcmp(e->d_name,"retention.anchor")))continue;
        if(count==UINT_MAX){bad=1;break;}
        if(count==capacity){
            if(capacity>SIZE_MAX/2/sizeof *names){bad=1;break;}
            void *grown=realloc(names,capacity*2*sizeof *names);if(!grown){bad=1;break;}
            names=grown;capacity*=2;
        }
        strcpy(names[count++],e->d_name);errno=0;
    }
    if(!e&&errno)bad=1;closedir(d);qsort(names,count,sizeof *names,sc_namecmp);
    struct gen_sha hash;gen_sha_init(&hash);
    for(unsigned i=0;i<count&&!bad;i++){
        struct stat before,after;char meta[160],value[65],path[PATH_MAX];
        if(fstatat(fd,names[i],&before,AT_SYMLINK_NOFOLLOW)||before.st_uid!=geteuid()){bad=1;break;}
        int n=snprintf(meta,sizeof meta,"%o\n",(unsigned)(before.st_mode&0177777));
        if(n<0||n>=(int)sizeof meta){bad=1;break;}
        gen_sha_add(&hash,names[i],strlen(names[i])+1);gen_sha_add(&hash,meta,(size_t)n);
        if(S_ISREG(before.st_mode)){
            struct migration_file file;memset(&file,0,sizeof file);bad=migration_read(fd,names[i],&file,0);
            if(!bad)gen_sha_add(&hash,file.sha,64);free(file.bytes);
        }else if(S_ISDIR(before.st_mode)){
            if((before.st_mode&07777)!=0700||sc_join(path,root,names[i])){bad=1;break;}
            /* fd may name a captured or not-yet-published tree. Following
             * root/name here would hash the other side of an exchange. */
            int child=openat(fd,names[i],O_RDONLY|O_DIRECTORY|O_NOFOLLOW|O_CLOEXEC);
            struct stat opened;
            if(child<0){bad=1;break;}
            if(fstat(child,&opened)||opened.st_dev!=before.st_dev||opened.st_ino!=before.st_ino||
               opened.st_mode!=before.st_mode||opened.st_uid!=before.st_uid){close(child);bad=1;break;}
            bad=sc_tree_hash_at(child,path,value);close(child);if(!bad)gen_sha_add(&hash,value,64);
        }else if(S_ISSOCK(before.st_mode)&&!strcmp(names[i],"control")&&(before.st_mode&07777)==0700&&before.st_nlink==1){
            n=snprintf(meta,sizeof meta,"terminal-control\n%llu\n%llu\n",(unsigned long long)before.st_dev,(unsigned long long)before.st_ino);
            if(n<0||n>=(int)sizeof meta)bad=1;else gen_sha_add(&hash,meta,(size_t)n);
        }else bad=1;
        if(fstatat(fd,names[i],&after,AT_SYMLINK_NOFOLLOW)||before.st_dev!=after.st_dev||before.st_ino!=after.st_ino||
           before.st_mode!=after.st_mode||before.st_uid!=after.st_uid||before.st_nlink!=after.st_nlink||before.st_size!=after.st_size)bad=1;
    }
    free(names);if(bad)return -1;gen_sha_end(&hash,sha);return 0;
}
/* A compact certificate explicitly supplies the previously validated logical
 * tree identity. Its retained bytes are verified separately; their physical
 * digest is never presented as equal to the old tree digest. */
static int rs_history_tree(int fd,const char *root,char sha[65]){
    char inventory[65];int compact=terminal_summary_read(fd,root,sha,inventory);
    return compact<0?-1:compact?0:rs_history_tree_raw(fd,root,sha,0);
}
/* Only explicitly retired ledgers may use this representation. Two immutable
 * records bind the old logical tree and the exact retained physical tree. A
 * missing/corrupt half is an error, never an invitation to recreate it. */
static int terminal_summary_read(int fd,const char *path,char old_tree[65],char inventory[65]){
    int present=bg_exists(fd,"retention.record"),anchor=bg_exists(fd,"retention.anchor");
    if(present<0||anchor<0||present!=anchor)return -1;if(!present)return 0;
    struct migration_file record;memset(&record,0,sizeof record);int result=-1;
    char scope[65]={0},retained[65]={0},extra,canonical[512],expected[128],actual[65];
    if(sc_file(fd,"retention.record",&record))goto done;
    int fields=sscanf(record.bytes,"BROray-terminal-summary/1\n%64s\n%64s\n%64s\n%64s\n%c",scope,old_tree,retained,inventory,&extra);
    if(fields!=4)return free(record.bytes),-1;
    int n=snprintf(canonical,sizeof canonical,"BROray-terminal-summary/1\n%s\n%s\n%s\n%s\n",scope,old_tree,retained,inventory);
    if(fields!=4||!hex64(scope)||!hex64(old_tree)||!hex64(retained)||!hex64(inventory)||n<0||
       (size_t)n!=record.size||memcmp(record.bytes,canonical,record.size))goto done;
    scope_digest(path,actual);if(strcmp(actual,scope))goto done;
    n=snprintf(expected,sizeof expected,"BROray-terminal-summary-anchor/1\n%s\n",record.sha);
    if(n<0||n>=(int)sizeof expected||bg_record_exact(fd,"retention.anchor",expected,(size_t)n)||
       rs_history_tree_raw(fd,path,actual,1)||strcmp(actual,retained))goto done;
    result=1;
done:free(record.bytes);return result;
}
static int terminal_copy(int from,int to,const char *name){
    struct migration_file file;memset(&file,0,sizeof file);int result=-1;
    if(sc_file(from,name,&file)||migration_record(to,name,file.bytes,file.size,1))goto done;
    result=0;
done:free(file.bytes);return result;
}
static int terminal_ready_pin(int source,int to,int directory,const char *id,const char *kind){
    char name[128],record[64];if(snprintf(name,sizeof name,"%s-%s.record",kind,id)>=(int)sizeof name)return -1;
    int present=bg_exists(directory,name);if(present<=0)return present;
    struct migration_file f;memset(&f,0,sizeof f);int result=-1;
    if(sc_file(directory,name,&f))goto done;
    const char *json=strchr(f.bytes,'{');unsigned long revision=0;char extra;
    const char *field=json?strstr(json,",\"revision\":"):NULL;
    if(!field||sscanf(field,",\"revision\":%lu%c",&revision,&extra)!=2||extra!=','||!revision||revision>1000000)goto done;
    record_name(revision,record);
    if(bg_record_exact(source,record,json,f.size-(size_t)(json-f.bytes))||terminal_copy(source,to,record))goto done;
    result=0;
done:free(f.bytes);return result;
}
static int terminal_ready_copies(int source,int to,const char *up,const char *id){
    int parent=checked_directory(up);if(parent<0)return -1;DIR *dir=directory_stream(parent);
    if(!dir){close(parent);return -1;}struct dirent *e;int bad=0;errno=0;
    while((e=readdir(dir))){
        if(strcmp(e->d_name,"cycles")&&strncmp(e->d_name,"cycles-op-",10))continue;
        char path[PATH_MAX];if(sc_join(path,up,e->d_name)){bad=1;break;}
        int fd=checked_directory(path);bad=fd<0||terminal_ready_pin(source,to,fd,id,"ready")||terminal_ready_pin(source,to,fd,id,"natural-stop");
        if(fd>=0)close(fd);if(bad)break;errno=0;
    }
    if(!e&&errno)bad=1;closedir(dir);close(parent);return bad?-1:0;
}
static int terminal_summary_publish(int fd,const char *path,const char *old_tree,const char *inventory){
    char scope[65],retained[65],text[512],sha[65],anchor[128];scope_digest(path,scope);
    if(rs_history_tree_raw(fd,path,retained,0))return -1;
    int n=snprintf(text,sizeof text,"BROray-terminal-summary/1\n%s\n%s\n%s\n%s\n",scope,old_tree,retained,inventory);
    if(n<0||n>=(int)sizeof text)return -1;digest_bytes(text,(size_t)n,sha);
    int a=snprintf(anchor,sizeof anchor,"BROray-terminal-summary-anchor/1\n%s\n",sha);
    if(a<0||a>=(int)sizeof anchor||migration_record(fd,"retention.record",text,(size_t)n,1)||
       migration_record(fd,"retention.anchor",anchor,(size_t)a,1)||fsync(fd))return -1;
    char checked[65],prior[65];return terminal_summary_read(fd,path,checked,prior)==1&&
        !strcmp(checked,old_tree)&&!strcmp(prior,inventory)?0:-1;
}
/* Reclaim only a tree already captured in the private retention namespace and
 * checked against its complete pre-exchange digest. No public pathname is
 * passed to unlink. The installation lifetime flock excludes updater writers;
 * retirement, not an elapsed timeout, proves that their generation ended. */
static int terminal_reclaim_contents(int fd,unsigned depth){
    if(depth>1)return -1;DIR *dir=directory_stream(fd);if(!dir)return -1;
    struct dirent *e;int bad=0;errno=0;
    while((e=readdir(dir))){
        if(!strcmp(e->d_name,".")||!strcmp(e->d_name,".."))continue;
        struct stat before,after;if(fstatat(fd,e->d_name,&before,AT_SYMLINK_NOFOLLOW)||before.st_uid!=geteuid()){bad=1;break;}
        if(S_ISDIR(before.st_mode)){
            if(depth||strcmp(e->d_name,"ledger-witnesses")||(before.st_mode&07777)!=0700){bad=1;break;}
            int child=openat(fd,e->d_name,O_RDONLY|O_DIRECTORY|O_NOFOLLOW|O_CLOEXEC);
            bad=child<0||fstat(child,&after)||before.st_ino!=after.st_ino||before.st_dev!=after.st_dev||terminal_reclaim_contents(child,depth+1);
            if(child>=0)close(child);if(bad||unlinkat(fd,e->d_name,AT_REMOVEDIR)){bad=1;break;}
        }else{
            if(before.st_nlink!=1||(!S_ISREG(before.st_mode)&&!(S_ISSOCK(before.st_mode)&&!strcmp(e->d_name,"control")))||
               fstatat(fd,e->d_name,&after,AT_SYMLINK_NOFOLLOW)||!sc_same_record(&before,&after)||unlinkat(fd,e->d_name,0)){bad=1;break;}
        }
        errno=0;
    }
    if(!e&&errno)bad=1;closedir(dir);return bad||fsync(fd)?-1:0;
}
static int terminal_start_compact(int generation_fd,int retained_fd,const char *domain,const char *up,struct retired_record *r){
    char last_name[64],startpath[PATH_MAX],hostpath[PATH_MAX],parent[PATH_MAX],path[PATH_MAX],stagepath[PATH_MAX];
    char born[65],wanted[65],tree[65],inventory[65],scope[65],expected[512];
    int start=-1,host=-1,witness=-1,starts=-1,stage=-1,to=-1,to_witness=-1,result=-1;
    struct migration_file last,launch,transaction,host_record;memset(&last,0,sizeof last);memset(&launch,0,sizeof launch);
    memset(&transaction,0,sizeof transaction);memset(&host_record,0,sizeof host_record);char *copy=NULL;
    record_name(r->total,last_name);if(sc_file(generation_fd,last_name,&last))goto done;
    if(strstr(last.bytes,"\"platformLaunch\":null")){result=0;goto done;}
    if(sc_join(parent,up,"starts")||sc_join(startpath,parent,r->gen)||sc_join(path,up,"hosts")||sc_join(hostpath,path,r->gen))goto done;
    starts=checked_directory(parent);start=checked_directory(startpath);host=checked_directory(hostpath);
    if(starts<0||start<0||host<0||flock(host,LOCK_EX|LOCK_NB)||sc_field(last.bytes,"bootId",born,sizeof born)||
       sc_file(start,"launch.record",&launch)||sc_field(last.bytes,"startIntentSha256",wanted,sizeof wanted)||strcmp(launch.sha,wanted)||
       sc_file(start,"transaction.record",&transaction)||sc_field(last.bytes,"transactionRecordSha256",wanted,sizeof wanted)||strcmp(transaction.sha,wanted)||
       sc_file(host,"host.record",&host_record)||sc_field(last.bytes,"serviceHostRecordSha256",wanted,sizeof wanted)||strcmp(host_record.sha,wanted))goto done;
    char *args[]={NULL,"service-host",hostpath,(char*)domain,r->gen,r->sha,NULL};
    if(service_retirement_text_at(host,args,host_record.bytes,host_record.size,expected,born)||bg_record_exact(host,"retirement.receipt",expected,strlen(expected)))goto done;
    copy=strdup(launch.bytes);if(!copy)goto done;char *cursor=copy,*rows[10];
    for(unsigned i=0;i<10;i++){rows[i]=sc_line(&cursor);if(!rows[i])goto done;}
    if(strcmp(rows[2],domain)||strcmp(rows[3],r->gen)||!migration_path(rows[1])||!token(rows[8],96))goto done;
    int nn=snprintf(path,sizeof path,"%s/opt/var/lib/broray/operations/%s/platform-replacement-start",strcmp(rows[1],"/")?rows[1]:"",rows[8]);
    if(nn<0||nn>=(int)sizeof path)goto done;
    int origin=checked_directory(path);
    if(origin>=0){int bad=terminal_ready_pin(generation_fd,retained_fd,origin,r->gen,"ready");close(origin);if(bad)goto done;}
    else if(errno!=ENOENT)goto done;
    int compact=terminal_summary_read(start,startpath,tree,inventory);if(compact<0)goto done;
    if(compact){if(strcmp(inventory,r->inventory))goto done;result=0;goto done;}
    if(sc_join(path,startpath,"ledger-witnesses"))goto done;witness=checked_directory(path);
    if(witness<0||ledger_inventory(witness,r->total,inventory)||rs_history_tree_raw(start,startpath,tree,0))goto done;
    for(unsigned long rev=1;rev<=r->total;rev++){
        char name[64],digest[65],*body=NULL;size_t size;record_name(rev,name);
        if(safe_bytes_at(generation_fd,name,&body,&size))goto done;digest_bytes(body,size,digest);free(body);
        int n=snprintf(expected,sizeof expected,"BROray-platform-ledger-witness/1\n%s\n%s\n%s\n%lu\n%s\n",r->gen,r->sha,born,rev,digest);
        if(n<0||n>=(int)sizeof expected||bg_record_exact(witness,name,expected,(size_t)n))goto done;
    }
    scope_digest(startpath,scope);
    if(snprintf(stagepath,sizeof stagepath,"%s/.broray-retention-%s",up,scope)>=(int)sizeof stagepath||mkdir(stagepath,0700))goto done;
    stage=checked_directory(stagepath);if(stage<0||migration_record(stage,"before.tree",tree,64,1)||mkdirat(stage,"replacement",0700)||fsync(stage))goto done;
    to=openat(stage,"replacement",O_RDONLY|O_DIRECTORY|O_NOFOLLOW|O_CLOEXEC);
    if(to<0||mkdirat(to,"ledger-witnesses",0700))goto done;
    to_witness=openat(to,"ledger-witnesses",O_RDONLY|O_DIRECTORY|O_NOFOLLOW|O_CLOEXEC);if(to_witness<0)goto done;
    DIR *dir=directory_stream(start);if(!dir)goto done;struct dirent *e;int bad=0;errno=0;
    while((e=readdir(dir))){
        const char *name=e->d_name;if(!strcmp(name,".")||!strcmp(name,"..")||!strcmp(name,"ledger-witnesses"))continue;
        if((strcmp(name,"launch.record")&&strcmp(name,"transaction.record")&&strcmp(name,"supervisor.log")&&strcmp(name,"service-host.log"))||terminal_copy(start,to,name)){bad=1;break;}errno=0;
    }
    if(!e&&errno)bad=1;closedir(dir);if(bad)goto done;
    dir=directory_stream(retained_fd);if(!dir)goto done;errno=0;
    while((e=readdir(dir))){
        if(strcmp(e->d_name,"state.json")&&strncmp(e->d_name,"revision-",9))continue;
        if(terminal_copy(witness,to_witness,e->d_name)){bad=1;break;}errno=0;
    }
    if(!e&&errno)bad=1;closedir(dir);if(bad||fsync(to_witness)||terminal_summary_publish(to,startpath,tree,r->inventory))goto done;
    char checked[65];if(rs_history_tree_raw(start,startpath,checked,0)||strcmp(tree,checked)||installation_lock_valid()||
       syscall(SYS_renameat2,starts,r->gen,stage,"replacement",2)||fsync(starts)||fsync(stage))goto done;
    int captured=openat(stage,"replacement",O_RDONLY|O_DIRECTORY|O_NOFOLLOW|O_CLOEXEC);
    bad=captured<0||rs_history_tree_raw(captured,startpath,checked,0)||strcmp(tree,checked)||terminal_reclaim_contents(captured,0);
    if(captured>=0)close(captured);
    if(bad||unlinkat(stage,"replacement",AT_REMOVEDIR)||migration_record(stage,"reclaimed.receipt",tree,64,1)||fsync(stage))goto done;
    result=0;
done:
    if(start>=0)close(start);if(host>=0)close(host);if(witness>=0)close(witness);if(starts>=0)close(starts);
    if(stage>=0)close(stage);if(to>=0)close(to);if(to_witness>=0)close(to_witness);
    free(last.bytes);free(launch.bytes);free(transaction.bytes);free(host_record.bytes);free(copy);return result;
}
/* First implementation deliberately separates publication from reclamation.
 * The original directory is captured by atomic exchange and checked AFTER
 * capture. Unknown bytes are retained under the private staging name. */
static int terminal_compact_impl(int argc,char **argv,int emit){
    if((argc!=7&&argc!=8)||!token(argv[3],64)||!hex64(argv[4])||!token(argv[5],96)||!token(argv[6],64))return 64;
    int source=-1,stage=-1,replacement=-1,result=75;struct retired_record r;
    char tree[65],inventory[65],scope[65],stagepath[PATH_MAX],up[PATH_MAX],leaf[65],last[64];
    source=checked_directory(argv[2]);if(source<0||installation_claim(argv[2])||
       retirement_valid(source,argv[2],&r)||strcmp(r.gen,argv[3])||strcmp(r.sha,argv[4])||
       strcmp(r.op,argv[5])||strcmp(r.nonce,argv[6]))goto done;
    int compact=terminal_summary_read(source,argv[2],tree,inventory);if(compact<0)goto done;
    if(!compact&&rs_history_tree_raw(source,argv[2],tree,0))goto done;
    strcpy(up,installation_path);char *slash=strrchr(up,'/');if(!slash||slash==up)goto done;*slash=0;
    const char *name=strrchr(argv[2],'/');if(!name||!token(name+1,64))goto done;strcpy(leaf,name+1);
    scope_digest(argv[2],scope);
    if(snprintf(stagepath,sizeof stagepath,"%s/.broray-retention-%s",up,scope)>=(int)sizeof stagepath)goto done;
    if(compact){
        /* The compact projection alone does not prove reclamation. In
         * particular, an interrupted capture may retain unknown bytes. */
        stage=checked_directory(stagepath);
        if(stage<0||bg_record_exact(stage,"before.tree",tree,64)||bg_exists(stage,"replacement")!=0||
           bg_record_exact(stage,"reclaimed.receipt",tree,64))goto done;
        result=0;goto done;
    }
    /* A pre-existing unfinished stage is preserved for explicit recovery. */
    if(mkdir(stagepath,0700))goto done;stage=checked_directory(stagepath);if(stage<0)goto done;
    if(migration_record(stage,"before.tree",tree,64,1)||mkdirat(stage,"replacement",0700)||fsync(stage))goto done;
    replacement=openat(stage,"replacement",O_RDONLY|O_DIRECTORY|O_NOFOLLOW|O_CLOEXEC);if(replacement<0)goto done;
    record_name(r.total,last);
    char ready[64]={0};
    if(argc==8){unsigned long number=0;char tail;
        if(sscanf(argv[7],"%lu%c",&number,&tail)!=1||!number||number>r.total)goto done;
        record_name(number,ready);
        if(strcmp(ready,"state.json")&&strcmp(ready,last)&&terminal_copy(source,replacement,ready))goto done;
    }
    if(terminal_copy(source,replacement,"state.json")||
       (strcmp(last,"state.json")&&terminal_copy(source,replacement,last))||
       terminal_copy(source,replacement,"retirement.receipt")||terminal_copy(source,replacement,"lifetime.lock")||
       (!strcmp(strrchr(installation_path,'/')+1,"generations")&&terminal_ready_copies(source,replacement,up,r.gen))||
       terminal_start_compact(source,replacement,argv[2],up,&r)||
       terminal_summary_publish(replacement,argv[2],tree,r.inventory)||retirement_valid(replacement,argv[2],NULL))goto done;
    char rechecked[65];if(rs_history_tree_raw(source,argv[2],rechecked,0)||strcmp(tree,rechecked)||installation_lock_valid())goto done;
    if(syscall(SYS_renameat2,installation_fd,leaf,stage,"replacement",2)||fsync(installation_fd)||fsync(stage))goto done;
    int captured=openat(stage,"replacement",O_RDONLY|O_DIRECTORY|O_NOFOLLOW|O_CLOEXEC);
    int bad=captured<0||rs_history_tree_raw(captured,argv[2],rechecked,0)||strcmp(tree,rechecked);
    if(captured>=0)close(captured);if(bad)goto done;
    captured=openat(stage,"replacement",O_RDONLY|O_DIRECTORY|O_NOFOLLOW|O_CLOEXEC);
    bad=captured<0||rs_history_tree_raw(captured,argv[2],rechecked,0)||strcmp(tree,rechecked)||
        terminal_reclaim_contents(captured,0);
    if(captured>=0)close(captured);
    if(bad||unlinkat(stage,"replacement",AT_REMOVEDIR)||migration_record(stage,"reclaimed.receipt",tree,64,1)||fsync(stage))goto done;
    result=0;
done:
    if(source>=0)close(source);if(stage>=0)close(stage);if(replacement>=0)close(replacement);
    if(installation_lock>=0){close(installation_lock);installation_lock=-1;}
    if(installation_fd>=0){close(installation_fd);installation_fd=-1;}
    if(!result&&emit)puts("{\"ok\":true,\"phase\":\"TERMINAL_HISTORY_COMPACTED\",\"storageReclaimed\":true}");
    if(result)fputs("TERMINAL_COMPACTION_UNCONFIRMED\n",stderr);
    return result;
}
static int terminal_compact_main(int argc,char **argv){return terminal_compact_impl(argc,argv,1);}
static void terminal_compact_installation(const char *up,const char *current){
    char parent[PATH_MAX];if(sc_join(parent,up,"generations"))return;
    int fd=checked_directory(parent);if(fd<0)return;DIR *dir=directory_stream(fd);
    if(!dir){close(fd);return;}struct dirent *e;
    while((e=readdir(dir))){
        if(!token(e->d_name,64)||!strcmp(e->d_name,current))continue;
        char path[PATH_MAX];if(sc_join(path,parent,e->d_name))break;
        int domain=checked_directory(path);if(domain<0)continue;
        struct retired_record r;int valid=retirement_valid(domain,path,&r)==0;close(domain);
        if(!valid)continue; /* Admission still verifies unresolved histories. */
        char *args[]={NULL,"compact-retired",path,r.gen,r.sha,r.op,r.nonce,NULL};
        if(terminal_compact_impl(7,args,0))fputs("TERMINAL_HISTORY_RETAINED\n",stderr);
    }
    closedir(dir);close(fd);
}
/* Bind all pre-existing updater history and the already validated transaction.
 * B's own lifecycle directories are excluded by its independently derived ID.
 * New/removed/changed foreign history invalidates the exact record on replay. */
static int rs_history(const char *id,char **text,size_t *size){
    FILE *out=open_memstream(text,size);if(!out)return -1;int bad=0;
    const char *families[]={"starts","generations","hosts"};
    fputs("BROray-platform-replacement-history/1\n",out);
    for(unsigned family=0;family<3&&!bad;family++){
        char path[PATH_MAX];if(sc_join(path,sc.uppath,families[family])){bad=1;break;}
        int fd=checked_directory(path);if(fd<0){bad=1;break;}
        DIR *d=directory_stream(fd);if(!d){close(fd);bad=1;break;}
        char names[SC_LIMIT][65];unsigned count=0;struct dirent *e;errno=0;
        while((e=readdir(d))){
            const char *n=e->d_name;
            if(!strcmp(n,".")||!strcmp(n,"..")||!strcmp(n,id))continue;
            int owned=0;if(sc.replacement)for(unsigned i=0;i<sc.count;i++)if(!strcmp(n,sc.nodes[i].id))owned=1;
            for(unsigned i=0;i<rs_owned_count;i++)if(!strcmp(n,rs_owned[i]))owned=1;
            if(owned)continue;
            if(!strcmp(families[family],"generations")&&!strcmp(n,".generation-lifetime.lock")){
                struct stat st;
                if(fstatat(fd,n,&st,AT_SYMLINK_NOFOLLOW)||!S_ISREG(st.st_mode)||st.st_uid!=geteuid()||st.st_nlink!=1||st.st_size||(st.st_mode&07777)!=0600){bad=1;break;}
                fprintf(out,"generation-exclusion\t%llu\t%llu\n",(unsigned long long)st.st_dev,(unsigned long long)st.st_ino);errno=0;continue;
            }
            if(count==SC_LIMIT||!token(n,64)){bad=1;break;}strcpy(names[count++],n);errno=0;
        }
        if(!e&&errno)bad=1;closedir(d);
        for(unsigned i=0;i<count;i++)for(unsigned j=i+1;j<count;j++)if(strcmp(names[i],names[j])>0){char temp[65];strcpy(temp,names[i]);strcpy(names[i],names[j]);strcpy(names[j],temp);}
        for(unsigned i=0;i<count&&!bad;i++){
            char full[PATH_MAX],sha[65];if(sc_join(full,path,names[i])){bad=1;break;}
            int child=checked_directory(full);if(child<0){bad=1;break;}
            bad=rs_history_tree(child,full,sha);close(child);
            if(!bad)fprintf(out,"%s/%s\t%s\n",families[family],names[i],sha);
        }
        close(fd);
    }
    /* Older lifecycle origins remain frozen evidence outside B's scope. */
    DIR *origins=directory_stream(sc.up);if(!origins)bad=1;
    if(origins){char names[SC_LIMIT][128];unsigned count=0;struct dirent *e;errno=0;
        while((e=readdir(origins))){
            if(strcmp(e->d_name,"cycles")&&strncmp(e->d_name,"cycles-op-",10))continue;
            char path[PATH_MAX];if(sc_join(path,sc.uppath,e->d_name)){bad=1;break;}
            if(!strcmp(path,sc.cyclepath)||(!strncmp(e->d_name,"cycles-",7)&&!strcmp(e->d_name+7,sc.origin)))continue;
            if(count==SC_LIMIT||strlen(e->d_name)>=sizeof names[0]){bad=1;break;}strcpy(names[count++],e->d_name);errno=0;
        }
        if(!e&&errno)bad=1;closedir(origins);
        for(unsigned i=0;i<count;i++)for(unsigned j=i+1;j<count;j++)if(strcmp(names[i],names[j])>0){char temp[128];strcpy(temp,names[i]);strcpy(names[i],names[j]);strcpy(names[j],temp);}
        for(unsigned i=0;i<count&&!bad;i++){
            char path[PATH_MAX],sha[65];if(sc_join(path,sc.uppath,names[i])){bad=1;break;}
            int fd=checked_directory(path);if(fd<0){bad=1;break;}
            bad=sc_tree_hash_at(fd,path,sha);close(fd);if(!bad)fprintf(out,"%s\t%s\n",names[i],sha);
        }
    }
    const char *fixed[]={"platform-replacement-backup","platform-replacement-target","platform-replacement-install"};
    for(unsigned i=0;i<3&&!bad;i++){
        char path[PATH_MAX],sha[65];if(sc_join(path,sc.oppath,fixed[i])){bad=1;break;}
        int fd=checked_directory(path);if(fd<0){bad=1;break;}
        bad=sc_tree_hash_at(fd,path,sha);close(fd);if(!bad)fprintf(out,"%s\t%s\n",fixed[i],sha);
    }
    if(fclose(out))bad=1;return bad?-1:0;
}
static int rs_prior_history(void){
    char *text=NULL;size_t size=0;int bad=rs_history(sc.initial,&text,&size);
    if(!bad)bad=bg_record_exact(sc.op,"platform-replacement-start/history.record",text,size);
    free(text);return bad;
}
static int rs_history_member(const char *family,const char *id){
    if(!token(id,64))return -1;
    for(unsigned i=0;i<rs_owned_count;i++)if(!strcmp(id,rs_owned[i]))return 0;
    struct migration_file f;memset(&f,0,sizeof f);char prefix[128];int result=-1;
    int n=snprintf(prefix,sizeof prefix,"%s/%s\t",family,id);
    if(n<0||n>=(int)sizeof prefix||sc_file(sc.op,"platform-replacement-start/history.record",&f))goto done;
    char *cursor=f.bytes,*line;
    while((line=sc_line(&cursor)))if(!strncmp(line,prefix,(size_t)n)&&hex64(line+n)){result=0;break;}
done:free(f.bytes);return result;
}
static int rs_launch(const char *intent,size_t intent_size,const char *manifest,size_t manifest_size,
        const char *id,const char *shell,const char *shell_sha,char **launch,size_t *ls,char **transaction,size_t *ts){
    char domain[PATH_MAX],parent[PATH_MAX];
    if(sc_join(parent,sc.uppath,"generations")||sc_join(domain,parent,id))return -1;
    FILE *f=open_memstream(launch,ls);if(!f)return -1;
    fprintf(f,"BROray-platform-launch/2\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n",sc.root,domain,id,sc.input.manifest,sc.native,shell,shell_sha,sc.origin,sc.nonce);
    fwrite(manifest,1,manifest_size,f);if(fclose(f))return -1;
    f=open_memstream(transaction,ts);if(!f)return -1;
    fprintf(f,"BROray-platform-replacement-start-intent/1\nSTART_INTENT\n%s\n",sc.seal);fwrite(intent,1,intent_size,f);return fclose(f)?-1:0;
}
/* Retain the authenticated coordinator closure for a supervised replacement.
 * This is explicitly not a legacy migration or an ended-boot receipt. The
 * descriptor and its independent anchor precede the write-once closure; a
 * partial publication is evidence requiring recovery, never a fresh origin. */
static int rs_code(const char *source){
    int src=-1,base=-1,code=-1,bin=-1,lib=-1,runtime=-1,result=-1;
    char directory[PATH_MAX],codepath[PATH_MAX],path[PATH_MAX],store[PATH_MAX];
    char descriptor[1024],anchor[160],receipt[128],sha[65],manifest_sha[65];
    char *manifest=NULL;size_t size=0;struct migration_file files[RC_FILES];memset(files,0,sizeof files);
    if(sc_join(directory,sc.oppath,"platform-replacement-code")||sc_join(codepath,directory,"code")||
       sc_join(store,sc.uppath,"runtimes")||sc_join(path,store,sc.native))goto done;
    runtime=checked_directory(path);if(runtime<0||runtime_names(runtime)||bg_runtime_valid(runtime,sc.native))goto done;
    int exists=bg_exists(sc.op,"platform-replacement-code"),bound=bg_exists(sc.op,"platform-replacement-service.json"),
        anchored=bg_exists(sc.op,"platform-replacement-service.anchor");
    if(exists<0||bound<0||anchored<0||exists!=bound||exists!=anchored||(!source&&!exists))goto done;
    src=source?migration_directory(source):checked_directory(codepath);
    if(src<0||rc_load(src,files,&manifest,&size,manifest_sha))goto done;
    int dn=snprintf(descriptor,sizeof descriptor,"{\"schemaVersion\":1,\"contract\":\"broray-replacement-service/1\",\"operationId\":\"%s\",\"stopNonce\":\"%s\",\"startIntentSha256\":\"%s\",\"nativeSha256\":\"%s\",\"platformManifestSha256\":\"%s\",\"codeManifestSha256\":\"%s\",\"processAuthority\":false}\n",sc.origin,sc.nonce,sc.seal,sc.native,sc.input.manifest,manifest_sha);
    if(dn<0||dn>=(int)sizeof descriptor)goto done;digest_bytes(descriptor,(size_t)dn,sha);
    int an=snprintf(anchor,sizeof anchor,"BROray-replacement-service-anchor/1\n%s\n",sha),
        rn=snprintf(receipt,sizeof receipt,"BROray-recovery-code-staged/1\n%s\n",manifest_sha);
    if(an<0||an>=(int)sizeof anchor||rn<0||rn>=(int)sizeof receipt||rc_exact(src,files)||
       migration_record(sc.op,"platform-replacement-service.anchor",anchor,(size_t)an,!exists)||
       migration_record(sc.op,"platform-replacement-service.json",descriptor,(size_t)dn,!exists))goto done;
    if(!exists&&(mkdirat(sc.op,"platform-replacement-code",0700)||fsync(sc.op)))goto done;
    base=checked_directory(directory);if(base<0||(!exists&&(bg_empty(base)||mkdirat(base,"code",0700)||fsync(base))))goto done;
    code=checked_directory(codepath);if(code<0||(!exists&&(bg_empty(code)||mkdirat(code,"bin",0700)||mkdirat(code,"lib",0700)||fsync(code))))goto done;
    if(sc_join(path,codepath,"bin"))goto done;bin=checked_directory(path);
    if(sc_join(path,codepath,"lib"))goto done;lib=checked_directory(path);
    if(bin<0||lib<0||(exists?rc_complete(base,code,bin,lib):(bg_empty(bin)||bg_empty(lib)))||
       migration_record(base,"manifest.record",manifest,size,!exists))goto done;
    for(int i=0;i<RC_FILES;i++){
        int fd=i?lib:bin;const char *name=rc_paths[i]+4;
        if((!exists&&bg_candidate(fd,name,files[i].bytes,files[i].size,files[i].mode))||
           bg_exact(fd,name,files[i].bytes,files[i].size,files[i].mode)||bg_sync_file(fd,name,files[i].mode))goto done;
    }
    if(migration_record(base,"staged.receipt",receipt,(size_t)rn,!exists)||rc_complete(base,code,bin,lib)||
       rc_exact(code,files)||rc_exact(src,files)||bg_record_exact(base,"manifest.record",manifest,size)||
       bg_record_exact(sc.op,"platform-replacement-service.json",descriptor,(size_t)dn)||
       bg_record_exact(sc.op,"platform-replacement-service.anchor",anchor,(size_t)an)||
       fsync(bin)||fsync(lib)||fsync(code)||migration_sync_directory(directory,base)||migration_sync_directory(sc.oppath,sc.op))goto done;
    result=0;
done:
    if(src>=0)close(src);if(base>=0)close(base);if(code>=0)close(code);if(bin>=0)close(bin);if(lib>=0)close(lib);if(runtime>=0)close(runtime);
    rc_free(files);free(manifest);return result;
}
static int rs_service_read(void){
    struct migration_file intent;memset(&intent,0,sizeof intent);char *copy=NULL;int result=-1;
    if(sc_file(sc.op,"platform-replacement-start/intent.record",&intent)||strcmp(intent.sha,sc.migration))goto done;
    copy=strdup(intent.bytes);if(!copy)goto done;char *cursor=copy,*rows[14];
    for(unsigned i=0;i<14;i++){rows[i]=sc_line(&cursor);if(!rows[i])goto done;}
    if(*cursor||strcmp(rows[0],"BROray-platform-replacement-start/1")||strcmp(rows[1],"START_INTENT")||
       strcmp(rows[2],sc.root)||strcmp(rows[3],sc.origin)||strcmp(rows[4],sc.nonce)||strcmp(rows[6],sc.native)||!hex64(rows[7]))goto done;
    strcpy(sc.seal,intent.sha);strcpy(sc.input.manifest,rows[7]);rs_owned_count=0;
    int saved_cycles=sc.cycles;char path[PATH_MAX],saved_path[PATH_MAX];strcpy(saved_path,sc.cyclepath);struct migration_file manifest;memset(&manifest,0,sizeof manifest);
    struct sc_node node;memset(&node,0,sizeof node);strcpy(node.id,rows[13]);
    if(sc_join(path,sc.oppath,"platform-replacement-start"))goto done;
    strcpy(sc.cyclepath,path);sc.cycles=checked_directory(path);
    int bad=sc.cycles<0||sc_file(sc.op,"platform-replacement-target/manifest.record",&manifest)||strcmp(manifest.sha,rows[7])||
        rs_birth(&node,0)||rs_attempts(&node,&intent,&manifest,rows[8],rows[9],0,0);
    if(sc.cycles>=0)close(sc.cycles);sc.cycles=saved_cycles;strcpy(sc.cyclepath,saved_path);free(manifest.bytes);
    if(bad)goto done;result=rs_code(NULL);
done:free(copy);free(intent.bytes);return result;
}
static int replacement_service_code_verify(const char *root,const char *origin,const char *start){
    char path[PATH_MAX],nonce[65];const char *prefix=strcmp(root,"/")?root:"";
    int op=-1,result=-1,context=0;struct migration_file binding;memset(&binding,0,sizeof binding);
    if(!migration_path(root)||!token(origin,96)||!hex64(start)||
       snprintf(path,sizeof path,"%s/opt/var/lib/broray/operations/%s",prefix,origin)>=(int)sizeof path)goto done;
    op=checked_directory(path);
    if(op<0||sc_file(op,"platform-replacement-service.json",&binding)||sc_field(binding.bytes,"stopNonce",nonce,sizeof nonce)||strlen(nonce)!=32)goto done;
    context=1;if(rs_context(root,origin,nonce))goto done;strcpy(sc.migration,start);result=rs_service_read();
done:if(context)sc_close();if(op>=0)close(op);free(binding.bytes);return result;
}
static int replacement_start_prepare(char **argv,int op,const char *op_path,const char *native){
    (void)op;(void)op_path;int result=75,starts=-1,start=-1;char path[PATH_MAX],parent[PATH_MAX],seed[768],seed_sha[65],id[65],history_sha[65];
    char anchor[256],binding[320],launch_sha[65],transaction_sha[65];
    char *history=NULL,*intent=NULL,*launch=NULL,*transaction=NULL;size_t hs=0,is=0,ls=0,ts=0;
    struct migration_file installed,manifest;memset(&installed,0,sizeof installed);memset(&manifest,0,sizeof manifest);
    if(rs_context(argv[6],argv[10],argv[11])||strcmp(native,sc.native)||rs_fence(argv[14])||
       sc_file(sc.op,"platform-replacement-install/installed.receipt",&installed)||
       sc_file(sc.op,"platform-replacement-target/manifest.record",&manifest)||strcmp(manifest.sha,argv[12]))goto done;
    strcpy(sc.input.manifest,argv[12]);
    int sn=snprintf(seed,sizeof seed,"BROray-platform-replacement-generation/1\n%s\n%s\n%s\n%s\n%s\n%s\n",sc.origin,sc.nonce,boot,sc.native,installed.sha,argv[14]);
    if(sn<0||sn>=(int)sizeof seed)goto done;digest_bytes(seed,(size_t)sn,seed_sha);platform_generation_id(seed_sha,id);
    if(rs_history(id,&history,&hs))goto done;digest_bytes(history,hs,history_sha);
    FILE *f=open_memstream(&intent,&is);if(!f)goto done;
    fprintf(f,"BROray-platform-replacement-start/1\nSTART_INTENT\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n",
            sc.root,sc.origin,sc.nonce,boot,sc.native,sc.input.manifest,argv[7],argv[8],argv[14],installed.sha,history_sha,id);
    if(fclose(f))goto done;digest_bytes(intent,is,sc.seal);
    if(rs_launch(intent,is,manifest.bytes,manifest.size,id,argv[7],argv[8],&launch,&ls,&transaction,&ts))goto done;
    digest_bytes(launch,ls,launch_sha);digest_bytes(transaction,ts,transaction_sha);
    int an=snprintf(anchor,sizeof anchor,"BROray-platform-replacement-start-anchor/1\n%s\n",sc.seal);
    int bn=snprintf(binding,sizeof binding,"BROray-platform-replacement-start-binding/1\n%s\n%s\n%s\n",sc.seal,launch_sha,transaction_sha);
    if(an<0||an>=(int)sizeof anchor||bn<0||bn>=(int)sizeof binding||sc_join(parent,sc.uppath,"starts")||sc_join(path,parent,id))goto done;
    starts=checked_directory(parent);if(starts<0)goto done;
    int exists=bg_exists(sc.op,"platform-replacement-start"),bound=bg_exists(sc.op,"platform-replacement-start.record"),present=bg_exists(starts,id);
    if(exists<0||bound<0||present<0||exists!=bound||exists!=present)goto done;
    if(!exists){
        if(migration_record(sc.op,"platform-replacement-start.record",binding,(size_t)bn,1)||mkdirat(sc.op,"platform-replacement-start",0700)||fsync(sc.op)||mkdirat(starts,id,0700)||fsync(starts))goto done;
    }
    sc.cycles=checked_directory(sc.cyclepath);start=checked_directory(path);if(sc.cycles<0||start<0||(!exists&&(bg_empty(sc.cycles)||bg_empty(start))))goto done;
    if(migration_record(sc.op,"platform-replacement-start.record",binding,(size_t)bn,0)||
       migration_record(sc.cycles,"intent.record",intent,is,!exists)||migration_record(sc.cycles,"history.record",history,hs,!exists)||
       migration_record(sc.cycles,"intent.anchor",anchor,(size_t)an,!exists)||
       migration_record(start,"launch.record",launch,ls,!exists)||migration_record(start,"transaction.record",transaction,ts,!exists)||
       migration_sync_directory(path,start)||migration_sync_directory(sc.cyclepath,sc.cycles)||rs_fence(argv[14]))goto done;
    sc.transition=sc_lock_exact(sc.cycles,"transition.lock",!exists);if(sc.transition<0)goto done;
    const char *names[]={"intent.record","history.record","intent.anchor","transition.lock"},*start_names[]={"launch.record","transaction.record"};
    if(rc_names(sc.cycles,names,4)||rc_names(start,start_names,2)||pl_load(path,launch_sha,transaction_sha,0))goto done;
    char source[PATH_MAX];const char *suffix="/share/updater-platform";size_t length=strlen(argv[15]),tail=strlen(suffix);
    if(length<=tail||length-tail>=sizeof source||strcmp(argv[15]+length-tail,suffix))goto done;
    memcpy(source,argv[15],length-tail);source[length-tail]=0;
    if(rs_code(source))goto done;
    printf("{\"ok\":true,\"phase\":\"START_INTENT\",\"generationId\":\"%s\",\"intentSha256\":\"%s\",\"platformReady\":false,\"activationAllowed\":false}\n",id,sc.seal);result=0;
done:
    free(installed.bytes);free(manifest.bytes);free(history);free(intent);free(launch);free(transaction);
    if(start>=0)close(start);if(starts>=0)close(starts);sc_close();return result?service_error("PLATFORM_REPLACEMENT_START_INTENT_UNCONFIRMED"):0;
}
static int rs_commit_record(struct sc_node *n,const char *ready,const char *history_sha,
                            const char *initial_state,int publish,char sha[65]){
    char record[PATH_MAX+1024],anchor[160];
    int rn=snprintf(record,sizeof record,"BROray-platform-replacement-committed/1\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n",
        sc.root,sc.origin,sc.nonce,n->born,sc.native,sc.input.manifest,n->id,sc.seal,ready,history_sha,initial_state);
    if(rn<0||rn>=(int)sizeof record)return -1;digest_bytes(record,(size_t)rn,sha);
    int an=snprintf(anchor,sizeof anchor,"BROray-platform-replacement-commit-anchor/1\n%s\n",sha);
    if(an<0||an>=(int)sizeof anchor)return -1;
    int a=bg_exists(sc.op,"platform-replacement-committed.anchor"),r=bg_exists(sc.op,"platform-replacement-committed.record");
    /* A partial/missing pair is evidence requiring recovery, never a fresh
     * commit. Replaying a completed publication only compares exact bytes. */
    if(a<0||r<0||a!=r||(!r&&!publish))return -1;
    if(migration_record(sc.op,"platform-replacement-committed.anchor",anchor,(size_t)an,!r)||
       migration_record(sc.op,"platform-replacement-committed.record",record,(size_t)rn,!r)||
       migration_sync_directory(sc.oppath,sc.op)||
       bg_record_exact(sc.op,"platform-replacement-committed.anchor",anchor,(size_t)an)||
       bg_record_exact(sc.op,"platform-replacement-committed.record",record,(size_t)rn))return -1;
    return 0;
}
/* Preparation may precede reboot without creating any process. Bind actual
 * execution to its own durable birth record; never rewrite the start intent.
 * Missing birth evidence is fresh only while both process domains are absent
 * and the exact launch directory still contains its two preparation records.
 * Once any runtime evidence exists, absence/corruption is a refusal. */
static int rs_birth(struct sc_node *n,int publish){
    int a=sc_record_exists(sc.cycles,"birth.anchor"),r=sc_record_exists(sc.cycles,"birth.record");
    struct migration_file saved;memset(&saved,0,sizeof saved);char record[384],anchor[160],sha[65],born[64];int result=-1;
    if(a<0||r<0||a!=r||(!r&&!publish))goto done;
    if(r){
        char seal[65],id[65],extra;
        if(sc_service_file(sc.cycles,"birth.record",&saved,NULL)||
           sscanf(saved.bytes,"BROray-replacement-birth/1\n%64s\n%64s\n%63s\n%c",seal,id,born,&extra)!=3||
           strcmp(seal,sc.seal)||strcmp(id,n->id)||strlen(born)!=36||!token(born,36))goto done;
    }else{
        const char *prepared[]={"intent.record","history.record","intent.anchor","transition.lock"};
        const char *launch[]={"launch.record","transaction.record"};struct stat st;
        if(rc_names(sc.cycles,prepared,4)||lstat(pl.host,&st)==0||errno!=ENOENT||lstat(pl.domain,&st)==0||errno!=ENOENT)goto done;
        int start=checked_directory(pl.startdir);if(start<0)goto done;int bad=rc_names(start,launch,2);close(start);if(bad)goto done;
        strcpy(born,boot);
    }
    int rn=snprintf(record,sizeof record,"BROray-replacement-birth/1\n%s\n%s\n%s\n",sc.seal,n->id,born);
    if(rn<0||rn>=(int)sizeof record||(r&&(saved.size!=(size_t)rn||memcmp(saved.bytes,record,(size_t)rn))))goto done;
    digest_bytes(record,(size_t)rn,sha);int an=snprintf(anchor,sizeof anchor,"BROray-replacement-birth-anchor/1\n%s\n",sha);
    if(an<0||an>=(int)sizeof anchor||sc_publish(sc.cycles,"birth.anchor",anchor,(size_t)an,!r)||
       sc_publish(sc.cycles,"birth.record",record,(size_t)rn,!r)||migration_sync_directory(sc.cyclepath,sc.cycles))goto done;
    strcpy(n->born,born);result=0;
done:free(saved.bytes);return result;
}
static int rs_pending_node(struct sc_node *n,struct gb_record *b,char sha[65]){
    char path[PATH_MAX],parent[PATH_MAX];struct migration_file f;memset(&f,0,sizeof f);int result=-1;
    if(sc_join(parent,sc.uppath,"generations")||sc_join(path,parent,n->id))return -1;
    int fd=checked_directory(path);
    if(fd<0||rs_pending_validate(fd,path,b)||strcmp(b->id,n->id)||strcmp(b->from,n->born)||strcmp(b->manifest,sc.input.manifest)||
       strcmp(b->seal,sc.seal)||strcmp(b->launch,n->launch)||strcmp(b->transaction,n->transaction)||sc_file(fd,"pending-boot-ended.receipt",&f))goto done;
    strcpy(n->host,b->host);strcpy(sha,f.sha);result=0;
done:if(fd>=0)close(fd);free(f.bytes);return result;
}
static int rs_pending_retire(struct sc_node *n){
    if(!strcmp(n->born,boot)||sc_load_node(n)||pl_exact())return -1;
    int domain=checked_directory(pl.domain),host=checked_directory(pl.host),gens=-1,whole=-1,life=-1,result=-1;
    struct migration_file first,h,saved;memset(&first,0,sizeof first);memset(&h,0,sizeof h);memset(&saved,0,sizeof saved);
    struct gb_record b;memset(&b,0,sizeof b);char path[PATH_MAX],text[2048],sha[65];
    if(domain<0||host<0||sc_join(path,sc.uppath,"generations"))goto done;gens=checked_directory(path);
    if(gens<0||(whole=sc_lock_exact(gens,".generation-lifetime.lock",0))<0||(life=sc_lock_exact(domain,"lifetime.lock",0))<0||
       flock(host,LOCK_EX|LOCK_NB)||sc_file(domain,"state.json",&first)||sc_file(host,"host.record",&h))goto done;
    strcpy(b.id,n->id);strcpy(b.from,n->born);strcpy(b.through,boot);strcpy(b.manifest,sc.input.manifest);
    strcpy(b.scope,"");scope_digest(pl.domain,b.scope);strcpy(b.seal,sc.seal);strcpy(b.ready,first.sha);
    strcpy(b.host,h.sha);strcpy(b.launch,n->launch);strcpy(b.transaction,n->transaction);
    int exists=sc_record_exists(domain,"pending-boot-ended.receipt");if(exists<0)goto done;
    if(exists){
        if(sc_service_file(domain,"pending-boot-ended.receipt",&saved,NULL))goto done;
        char *copy=strdup(saved.bytes);if(!copy)goto done;char *cursor=copy,*row=NULL;
        for(int i=0;i<5;i++){row=sc_line(&cursor);if(!row)break;}
        int valid=row&&strlen(row)==36&&token(row,36)&&strcmp(row,n->born);if(valid)strcpy(b.through,row);free(copy);if(!valid)goto done;
    }
    if(rs_pending_measure(domain,pl.domain,&b))goto done;int k=rs_pending_text(&b,text);
    if(k<0||k>=2048||sc_publish(domain,"pending-boot-ended.receipt",text,(size_t)k,1)||rs_pending_node(n,&b,sha))goto done;
    result=0;
done:if(life>=0)close(life);if(whole>=0)close(whole);if(gens>=0)close(gens);if(host>=0)close(host);if(domain>=0)close(domain);free(first.bytes);free(h.bytes);free(saved.bytes);return result;
}
/* PID projections are archived only as bytes already bound to this ended
 * generation. They provide no signal authority. Absent projections are legal
 * before READY; a durable presence mask makes interrupted archival replayable. */
static int rs_pending_markers(struct sc_node *n,int publish){
    struct gb_record b;char proof[65],name[128],dir_name[128],path[PATH_MAX],parent[PATH_MAX],record[256],expected[32];
    struct migration_file latest,saved;memset(&latest,0,sizeof latest);memset(&saved,0,sizeof saved);
    int domain=-1,archive=-1,result=-1;unsigned mask=0;long pid=0;
    if(rs_pending_node(n,&b,proof)||sc_join(parent,sc.uppath,"generations")||sc_join(path,parent,n->id))goto done;
    domain=checked_directory(path);record_name(b.total,name);
    if(domain<0||sc_file(domain,name,&latest)||strcmp(latest.sha,b.last))goto done;
    const char *u=strstr(latest.bytes,",\"updater\":{\"pid\":");
    if(!u||sscanf(u,",\"updater\":{\"pid\":%ld,",&pid)!=1||pid<=1||pid>INT_MAX)goto done;
    int k=snprintf(expected,sizeof expected,"%ld\n",pid);if(k<=0||k>=(int)sizeof expected)goto done;
    snprintf(name,sizeof name,"residue-%s.record",n->id);snprintf(dir_name,sizeof dir_name,"residue-%s",n->id);
    int exists=sc_record_exists(sc.cycles,name);if(exists<0||(!exists&&!publish))goto done;
    const char *files[]={"daemon.pid","daemon.ready","daemon.lock"};
    if(exists){
        char observed[65],extra;long p;
        if(sc_service_file(sc.cycles,name,&saved,NULL)||sscanf(saved.bytes,"BROray-pending-residue/1\n%64s\n%ld\n%u\n%c",observed,&p,&mask,&extra)!=3||strcmp(observed,proof)||p!=pid||mask>7)goto done;
    }else for(unsigned i=0;i<3;i++){int present=bg_exists(sc.up,files[i]);if(present<0)goto done;if(present)mask|=1U<<i;}
    int rn=snprintf(record,sizeof record,"BROray-pending-residue/1\n%s\n%ld\n%u\n",proof,pid,mask);
    if(rn<0||rn>=(int)sizeof record||(exists&&(saved.size!=(size_t)rn||memcmp(saved.bytes,record,(size_t)rn))))goto done;
    if(sc_join(path,sc.cyclepath,dir_name))goto done;
    /* Validate all existing source markers before publishing any intent. */
    if(!exists)for(unsigned i=0;i<3;i++)if(mask&(1U<<i)){
        if(i<2){if(bg_record_exact(sc.up,files[i],expected,(size_t)k))goto done;}
        else{int lock=openat(sc.up,files[i],O_RDONLY|O_DIRECTORY|O_NOFOLLOW|O_CLOEXEC);struct stat st;
            int bad=lock<0;if(!bad)bad=fstat(lock,&st)||st.st_uid!=geteuid()||(st.st_mode&07777)!=0700||bg_empty(lock);if(lock>=0)close(lock);if(bad)goto done;}
    }
    if(sc_publish(sc.cycles,name,record,(size_t)rn,publish))goto done;
    archive=sc_directory(sc.cycles,dir_name,path,publish);if(archive<0)goto done;
    DIR *d=directory_stream(archive);if(!d)goto done;struct dirent *e;int bad=0;errno=0;
    while((e=readdir(d))){if(strcmp(e->d_name,".")&&strcmp(e->d_name,"..")&&strcmp(e->d_name,files[0])&&strcmp(e->d_name,files[1])&&strcmp(e->d_name,files[2])){bad=1;break;}errno=0;}
    if(!e&&errno)bad=1;closedir(d);if(bad)goto done;
    for(unsigned i=0;i<3;i++){
        int z=bg_exists(archive,files[i]),a=publish?bg_exists(sc.up,files[i]):0;if(z<0||a<0||a+z!=!!(mask&(1U<<i)))goto done;
        if(!(mask&(1U<<i)))continue;int dir=a?sc.up:archive;struct stat before,after;
        if(fstatat(dir,files[i],&before,AT_SYMLINK_NOFOLLOW)||before.st_uid!=geteuid())goto done;
        if(i<2){if(bg_record_exact(dir,files[i],expected,(size_t)k))goto done;}
        else{int lock=openat(dir,files[i],O_RDONLY|O_DIRECTORY|O_NOFOLLOW|O_CLOEXEC);
            if(lock<0)goto done;bad=(before.st_mode&07777)!=0700||bg_empty(lock);close(lock);if(bad)goto done;}
        if(a&&(syscall(SYS_renameat2,sc.up,files[i],archive,files[i],1)||fsync(sc.up)||fsync(archive)||
            fstatat(archive,files[i],&after,AT_SYMLINK_NOFOLLOW)||!sc_same_record(&before,&after)||before.st_nlink!=after.st_nlink))goto done;
    }
    result=0;
done:if(domain>=0)close(domain);if(archive>=0)close(archive);free(latest.bytes);free(saved.bytes);return result;
}
static int rs_attempt_names(unsigned attempts){
    DIR *d=directory_stream(sc.cycles);if(!d)return -1;struct dirent *e;int bad=0;errno=0;
    while((e=readdir(d))){const char *name=e->d_name;if(!strcmp(name,".")||!strcmp(name,".."))continue;
        int allowed=!strcmp(name,"intent.record")||!strcmp(name,"history.record")||!strcmp(name,"intent.anchor")||!strcmp(name,"birth.record")||!strcmp(name,"birth.anchor")||!strcmp(name,"transition.lock");
        char want[128];for(unsigned i=1;!allowed&&i<=attempts;i++){snprintf(want,sizeof want,"attempt-%020u.record",i);allowed=!strcmp(name,want);}
        for(unsigned i=0;!allowed&&i<rs_owned_count;i++){
            snprintf(want,sizeof want,"ready-%s.record",rs_owned[i]);allowed=!strcmp(name,want);
            if(!allowed){snprintf(want,sizeof want,"residue-%s.record",rs_owned[i]);allowed=!strcmp(name,want);}
            if(!allowed){snprintf(want,sizeof want,"residue-%s",rs_owned[i]);allowed=!strcmp(name,want);}
            if(!allowed&&i){snprintf(want,sizeof want,"birth-%s.record",rs_owned[i]);allowed=!strcmp(name,want);}
            if(!allowed&&i){snprintf(want,sizeof want,"birth-%s.anchor",rs_owned[i]);allowed=!strcmp(name,want);}
        }
        if(!allowed){bad=1;break;}errno=0;
    }
    if(!e&&errno)bad=1;closedir(d);return bad?-1:0;
}
static int rs_attempt_record(struct sc_node *previous,const char *born,unsigned index,struct sc_node *next,char text[640]){
    struct gb_record b;char proof[65],seed[512],sha[65];
    if(!previous->born[0]||strlen(born)!=36||!token(born,36)||!strcmp(born,previous->born)||rs_pending_node(previous,&b,proof))return -1;
    int k=snprintf(seed,sizeof seed,"BROray-replacement-retry/1\n%s\n%u\n%s\n%s\n%s\n",sc.seal,index,previous->id,proof,born);
    if(k<0||k>=(int)sizeof seed)return -1;digest_bytes(seed,(size_t)k,sha);memset(next,0,sizeof *next);
    platform_generation_id(sha,next->id);strcpy(next->born,born);
    k=snprintf(text,640,"%s%s\n",seed,next->id);
    if(k<=0||k>=640)return -1;digest_bytes(text,(size_t)k,next->chain);return k;
}
/* A retry intent is preparation, not proof of process birth. As for the
 * original start, bind execution separately without rewriting its intent.
 * Return 1 only for an intact, unexecuted preparation. A missing pair after
 * any host/generation evidence, or a partial pair, is never recreated. */
static int rs_retry_birth(struct sc_node *n,int publish){
    char name[128],anchor_name[128],record[512],anchor[160],sha[65],born[64];
    struct migration_file saved;memset(&saved,0,sizeof saved);int result=-1;
    snprintf(name,sizeof name,"birth-%s.record",n->id);snprintf(anchor_name,sizeof anchor_name,"birth-%s.anchor",n->id);
    int a=sc_record_exists(sc.cycles,anchor_name),r=sc_record_exists(sc.cycles,name);
    if(!hex64(n->chain)||a<0||r<0||a!=r)goto done;
    if(r){
        char seal[65],id[65],attempt[65],extra;
        if(sc_service_file(sc.cycles,name,&saved,NULL)||
           sscanf(saved.bytes,"BROray-replacement-retry-birth/1\n%64s\n%64s\n%64s\n%63s\n%c",seal,id,attempt,born,&extra)!=4||
           strcmp(seal,sc.seal)||strcmp(id,n->id)||strcmp(attempt,n->chain)||strlen(born)!=36||!token(born,36))goto done;
    }else{
        char path[PATH_MAX],parent[PATH_MAX];struct stat st;
        for(unsigned i=0;i<2;i++){
            if(sc_join(parent,sc.uppath,i?"hosts":"generations")||sc_join(path,parent,n->id)||
               lstat(path,&st)==0||errno!=ENOENT)goto done;
        }
        if(sc_join(parent,sc.uppath,"starts")||sc_join(path,parent,n->id))goto done;
        if(lstat(path,&st)==0){
            const char *names[]={"launch.record","transaction.record"};
            int start=checked_directory(path);if(start<0)goto done;
            int bad=rc_names(start,names,2);close(start);if(bad||sc_load_node(n))goto done;
        }else if(errno!=ENOENT||publish)goto done;
        if(!publish){n->born[0]=0;result=1;goto done;}
        strcpy(born,boot);
    }
    int rn=snprintf(record,sizeof record,"BROray-replacement-retry-birth/1\n%s\n%s\n%s\n%s\n",sc.seal,n->id,n->chain,born);
    if(rn<=0||rn>=(int)sizeof record||(r&&(saved.size!=(size_t)rn||memcmp(saved.bytes,record,(size_t)rn))))goto done;
    digest_bytes(record,(size_t)rn,sha);int an=snprintf(anchor,sizeof anchor,"BROray-replacement-retry-birth-anchor/1\n%s\n",sha);
    if(an<=0||an>=(int)sizeof anchor||sc_publish(sc.cycles,anchor_name,anchor,(size_t)an,!r)||
       sc_publish(sc.cycles,name,record,(size_t)rn,!r)||migration_sync_directory(sc.cyclepath,sc.cycles))goto done;
    strcpy(n->born,born);result=0;
done:free(saved.bytes);return result;
}
static int rs_attempt_materialize(struct sc_node *n,const struct migration_file *intent,const struct migration_file *manifest,const char *shell,const char *shellsha,int publish){
    char *launch=NULL,*transaction=NULL;size_t ls=0,ts=0;char path[PATH_MAX],parent[PATH_MAX],domain[PATH_MAX],host[PATH_MAX];
    int starts=-1,fd=-1,result=-1;
    if(rs_launch(intent->bytes,intent->size,manifest->bytes,manifest->size,n->id,shell,shellsha,&launch,&ls,&transaction,&ts))goto done;
    digest_bytes(launch,ls,n->launch);digest_bytes(transaction,ts,n->transaction);
    if(!publish){result=0;goto done;}
    if(sc_join(parent,sc.uppath,"starts")||sc_join(path,parent,n->id))goto done;
    starts=checked_directory(parent);if(starts<0)goto done;fd=sc_directory(starts,n->id,path,1);if(fd<0)goto done;
    if(sc_join(parent,sc.uppath,"generations")||sc_join(domain,parent,n->id)||sc_join(parent,sc.uppath,"hosts")||sc_join(host,parent,n->id))goto done;
    struct stat st;int born=lstat(domain,&st)==0||lstat(host,&st)==0;
    if(sc_publish(fd,"transaction.record",transaction,ts,!born)||sc_publish(fd,"launch.record",launch,ls,!born))goto done;
    result=0;
done:if(fd>=0)close(fd);if(starts>=0)close(starts);free(launch);free(transaction);return result;
}
static int rs_attempts(struct sc_node *n,const struct migration_file *intent,const struct migration_file *manifest,const char *shell,const char *shellsha,int publish,int materialize){
    rs_owned_count=0;strcpy(rs_owned[rs_owned_count++],n->id);
    if(rs_attempt_materialize(n,intent,manifest,shell,shellsha,0))return -1;
    unsigned index=1;
    for(;index<SC_LIMIT;index++){
        char name[80],record[640];snprintf(name,sizeof name,"attempt-%020u.record",index);
        int exists=sc_record_exists(sc.cycles,name);if(exists<0)return -1;if(!exists)break;
        struct migration_file f;memset(&f,0,sizeof f);char *copy=NULL;struct sc_node next;int bad=1;
        if(!sc_service_file(sc.cycles,name,&f,NULL)){
            copy=strdup(f.bytes);if(copy){char *cursor=copy,*row=NULL;
                for(unsigned i=0;i<6;i++){row=sc_line(&cursor);if(!row)break;}
                int k=row?rs_attempt_record(n,row,index,&next,record):-1;
                if(k>0&&k<640&&f.size==(size_t)k&&!memcmp(f.bytes,record,(size_t)k)&&
                   !rs_pending_markers(n,0)&&!sc_publish(sc.cycles,name,record,(size_t)k,0))bad=0;
            }
        }
        free(copy);free(f.bytes);if(bad)return -1;
        for(unsigned i=0;i<rs_owned_count;i++)if(!strcmp(next.id,rs_owned[i]))return -1;
        *n=next;strcpy(rs_owned[rs_owned_count++],n->id);
        if(rs_attempt_materialize(n,intent,manifest,shell,shellsha,0)||rs_retry_birth(n,0)<0)return -1;
    }
    if(index==SC_LIMIT||rs_attempt_names(index-1))return -1;
    /* Validate the unchanged pre-replacement history BEFORE publishing the
     * boot receipt, archival intent or a successor launch. */
    if(materialize){
        char *history=NULL;size_t hs=0;int bad=rs_history(n->id,&history,&hs);
        if(!bad)bad=bg_record_exact(sc.cycles,"history.record",history,hs);free(history);if(bad)return -1;
    }
    /* Service discovery only authenticates this immutable attempt chain.
     * sc_collect subsequently validates the same history with ALL sealed
     * ordinary service cycles loaded. Checking it before that collection
     * would misclassify a legitimate later cycle as foreign history. */
    if(publish&&n->born[0]&&strcmp(n->born,boot)){
        if(sc_record_exists(sc.op,"platform-replacement-committed.record")!=0||sc_record_exists(sc.op,"platform-replacement-committed.anchor")!=0||
           rs_pending_retire(n)||rs_pending_markers(n,1))return -1;
        struct sc_node next;char record[640],name[80];int k=rs_attempt_record(n,boot,index,&next,record);
        snprintf(name,sizeof name,"attempt-%020u.record",index);
        if(k<=0||k>=640||sc_publish(sc.cycles,name,record,(size_t)k,1))return -1;
        *n=next;strcpy(rs_owned[rs_owned_count++],n->id);
    }
    if(rs_attempt_materialize(n,intent,manifest,shell,shellsha,materialize&&publish))return -1;
    return rs_owned_count>1?rs_retry_birth(n,materialize&&publish):0;
}

static int replacement_start_main(int argc,char **argv){
    /* LIVE OP NONCE EXPECTED_MANIFEST STATE_SHA */
    if(argc!=7||!hex64(argv[5])||!hex64(argv[6]))return 64;
    int origin_check=!strcmp(argv[1],"replacement-origin-check");
    int committing=!strcmp(argv[1],"replacement-commit"),checking=origin_check||!strcmp(argv[1],"replacement-commit-check");
    int starting=!committing&&!checking;
    int result=75;char *copy=NULL,*history=NULL,*launch=NULL,*transaction=NULL;size_t hs=0,ls=0,ts=0;
    struct migration_file intent,manifest,installed;memset(&intent,0,sizeof intent);memset(&manifest,0,sizeof manifest);memset(&installed,0,sizeof installed);
    char launch_sha[65],transaction_sha[65],binding[320],anchor[256],history_sha[65],ready[65];
    if(rs_context(argv[2],argv[3],argv[4])||rs_fence_check(argv[6],checking))goto done;
    sc.cycles=checked_directory(sc.cyclepath);
    if(sc.cycles<0||sc_file(sc.cycles,"intent.record",&intent)||sc_file(sc.op,"platform-replacement-target/manifest.record",&manifest)||
       sc_file(sc.op,"platform-replacement-install/installed.receipt",&installed))goto done;
    copy=strdup(intent.bytes);if(!copy)goto done;char *cursor=copy,*rows[14];
    for(unsigned i=0;i<14;i++){rows[i]=sc_line(&cursor);if(!rows[i])goto done;}
    if(*cursor||strcmp(rows[0],"BROray-platform-replacement-start/1")||strcmp(rows[1],"START_INTENT")||
       strcmp(rows[2],sc.root)||strcmp(rows[3],sc.origin)||strcmp(rows[4],sc.nonce)||strlen(rows[5])!=36||!token(rows[5],36)||
       strcmp(rows[6],sc.native)||strcmp(rows[7],argv[5])||strcmp(manifest.sha,argv[5])||
       !migration_path(rows[8])||!hex64(rows[9])||!hex64(rows[10])||(!checking&&strcmp(rows[10],argv[6]))||strcmp(rows[11],installed.sha)||
       !hex64(rows[12])||!token(rows[13],64))goto done;
    strcpy(sc.input.manifest,argv[5]);strcpy(sc.seal,intent.sha);
    struct sc_node *n=&sc.nodes[0];strcpy(n->id,rows[13]);strcpy(n->born,rows[5]);sc.count=sc.baseline=1;
    if(rs_launch(intent.bytes,intent.size,manifest.bytes,manifest.size,n->id,rows[8],rows[9],&launch,&ls,&transaction,&ts))goto done;
    digest_bytes(launch,ls,launch_sha);digest_bytes(transaction,ts,transaction_sha);
    strcpy(n->launch,launch_sha);strcpy(n->transaction,transaction_sha);
    int an=snprintf(anchor,sizeof anchor,"BROray-platform-replacement-start-anchor/1\n%s\n",sc.seal);
    int bn=snprintf(binding,sizeof binding,"BROray-platform-replacement-start-binding/1\n%s\n%s\n%s\n",sc.seal,launch_sha,transaction_sha);
    if(an<0||an>=(int)sizeof anchor||bn<0||bn>=(int)sizeof binding||
       bg_record_exact(sc.cycles,"intent.anchor",anchor,(size_t)an)||bg_record_exact(sc.op,"platform-replacement-start.record",binding,(size_t)bn)||sc_load_node(n))goto done;
    sc.transition=sc_lock_exact(sc.cycles,"transition.lock",0);if(sc.transition<0)goto done;
    char ready_name[128];int nn=snprintf(ready_name,sizeof ready_name,"ready-%s.record",n->id);
    if(nn<0||nn>=(int)sizeof ready_name)goto done;
    const char *names[]={"intent.record","history.record","intent.anchor","transition.lock","birth.anchor","birth.record",ready_name};
    (void)names;
    if(rs_birth(n,starting)||rs_attempts(n,&intent,&manifest,rows[8],rows[9],starting,1))goto done;
    if(!origin_check&&strcmp(n->born,boot))goto done;
    if(rs_history(n->id,&history,&hs))goto done;digest_bytes(history,hs,history_sha);
    if(strcmp(history_sha,rows[12])||bg_record_exact(sc.cycles,"history.record",history,hs)||sc_load_node(n))goto done;
    nn=snprintf(ready_name,sizeof ready_name,"ready-%s.record",n->id);if(nn<0||nn>=(int)sizeof ready_name)goto done;
    if(origin_check&&strcmp(n->born,boot)){
        char commit_sha[65];struct migration_file pinned;memset(&pinned,0,sizeof pinned);
        struct gb_record ended;memset(&ended,0,sizeof ended);int domain=-1,bad=1;
        if(sc_file(sc.cycles,ready_name,&pinned)||
           sc_field(pinned.bytes,"serviceHostRecordSha256",n->host,sizeof n->host)||!hex64(n->host))goto historical_done;
        strcpy(ended.id,n->id);strcpy(ended.manifest,sc.input.manifest);strcpy(ended.from,n->born);strcpy(ended.through,boot);
        strcpy(ended.seal,sc.seal);strcpy(ended.ready,pinned.sha);strcpy(ended.host,n->host);
        strcpy(ended.launch,n->launch);strcpy(ended.transaction,n->transaction);scope_digest(pl.domain,ended.scope);
        domain=checked_directory(pl.domain);
        if(domain<0||rs_commit_record(n,pinned.sha,rows[12],rows[10],0,commit_sha)||
           gb_measure_ledger(domain,pl.domain,&ended,&pinned,sc.native)||pl_exact()||rs_fence_check(argv[6],1))goto historical_done;
        printf("{\"ok\":true,\"phase\":\"COMMITTED_BOOT_ENDED\",\"generationId\":\"%s\",\"commitReceiptSha256\":\"%s\",\"platformReady\":false,\"activationAllowed\":false}\n",n->id,commit_sha);bad=0;
historical_done:
        if(domain>=0)close(domain);free(pinned.bytes);if(!bad)result=0;goto done;
    }
    if(origin_check&&!strcmp(n->born,boot)){
        char *status[]={pl.nativepath,"control",pl.domain,"STATUS",n->id,sc.input.manifest,sc.origin,sc.nonce,NULL};
        if(!control_exchange(8,status,0)&&strstr(snapshot,"\"state\":\"STOPPED\"")&&
           strstr(snapshot,"\"children\":[],\"awaitingBirth\":[],\"exitedUnreaped\":[]")&&
           strstr(snapshot,"\"platformReady\":false")){
            char commit_sha[65];
            if(sc_field(snapshot,"serviceHostRecordSha256",n->host,sizeof n->host)||!hex64(n->host)||
               sc_host_record(n)||sc_ready(n,0,ready)||rs_commit_record(n,ready,rows[12],rows[10],0,commit_sha)||pl_exact()||rs_fence_check(argv[6],1))goto done;
            printf("{\"ok\":true,\"phase\":\"COMMITTED_STOPPED\",\"generationId\":\"%s\",\"commitReceiptSha256\":\"%s\",\"platformReady\":false,\"activationAllowed\":false}\n",n->id,commit_sha);result=0;goto done;
        }
    }
    int replay=!sc_live(n);
    if(!replay&&(!starting||sc_start(n)))goto done;
    if(sc_live(n)||sc_ready(n,starting,ready)||rs_fence_check(argv[6],checking)||
       bg_record_exact(sc.cycles,"intent.record",intent.bytes,intent.size)||bg_record_exact(sc.cycles,"intent.anchor",anchor,(size_t)an)||
       bg_record_exact(sc.op,"platform-replacement-start.record",binding,(size_t)bn))goto done;
    free(history);history=NULL;hs=0;if(rs_history(n->id,&history,&hs)||bg_record_exact(sc.cycles,"history.record",history,hs))goto done;
    if(!starting){
        char commit_sha[65];
        if(rs_commit_record(n,ready,rows[12],rows[10],committing,commit_sha)||sc_live(n)||
           sc_ready(n,0,ready)||rs_fence_check(argv[6],checking))goto done;
        printf("{\"ok\":true,\"phase\":\"%s\",\"generationId\":\"%s\",\"commitReceiptSha256\":\"%s\",\"platformReady\":true,\"activationAllowed\":false}\n",
               committing?"COMMITTED":"COMMIT_VERIFIED",n->id,commit_sha);result=0;goto done;
    }
    printf("{\"ok\":true,\"phase\":\"READY\",\"generationId\":\"%s\",\"readinessSha256\":\"%s\",\"replayed\":%s,\"platformReady\":true,\"activationAllowed\":false}\n",n->id,ready,replay?"true":"false");result=0;
done:
    free(copy);free(history);free(launch);free(transaction);free(intent.bytes);free(manifest.bytes);free(installed.bytes);sc_close();
    return result?service_replacement_error(starting?"PLATFORM_REPLACEMENT_START_UNCONFIRMED":"PLATFORM_REPLACEMENT_COMMIT_UNCONFIRMED"):0;
}
/* A lifecycle READY projection may be initialized after boot only from the
 * original immutable COMMITTED pin. No generation ledger is reconstructed.
 * The canonical coordinator rechecks completed state/fence/retained closure;
 * its native historical proof checks the entire ledger and witness chain. */
static int rs_initial_boot_ready(char **argv,struct sc_node *n){
    char name[128],source[256],prefix[512],reply[8192],id[65];
    struct migration_file original;memset(&original,0,sizeof original);char *text=NULL;size_t size=0;int result=-1;
    int nn=snprintf(name,sizeof name,"ready-%s.record",n->id);
    int sn=snprintf(source,sizeof source,"platform-replacement-start/%s",name);
    int pn=snprintf(prefix,sizeof prefix,"BROray-service-ready/1\n%s\n%s\n%s\n%s\n",sc.migration,n->launch,n->transaction,n->host);
    if(nn<0||nn>=(int)sizeof name||sn<0||sn>=(int)sizeof source||pn<0||pn>=(int)sizeof prefix||
       sc_file(sc.op,source,&original)||original.size<=(size_t)pn||memcmp(original.bytes,prefix,(size_t)pn))goto done;
    FILE *f=open_memstream(&text,&size);if(!f)goto done;
    fprintf(f,"BROray-service-ready/1\n%s\n%s\n%s\n%s\n",sc.seal,n->launch,n->transaction,n->host);
    fwrite(original.bytes+pn,1,original.size-(size_t)pn,f);if(fclose(f))goto done;
    int exists=sc_record_exists(sc.cycles,name);if(exists<0)goto done;
    if(exists){result=sc_publish(sc.cycles,name,text,size,0);goto done;}
    if(!sc.may_publish||sc_record_exists(sc.cycles,"cycle-00000000000000000001.record")!=0||
       sc_call(argv,"recovery-commit-check",reply,sizeof reply)||
       !(strstr(reply,"\"phase\":\"COMMITTED_BOOT_ENDED\"")||strstr(reply,"\"phase\":\"COMMITTED_STOPPED\""))||!strstr(reply,"\"platformReady\":false")||
       sc_field(reply,"generationId",id,sizeof id)||strcmp(id,n->id)||
       sc_publish(sc.cycles,name,text,size,1))goto done;
    result=0;
done:free(original.bytes);free(text);return result;
}
/* Installed S22 entry. Discovery supplies identifiers, not execution authority.
 * Authenticate the retained closure and the exact opened runtime before any
 * shell exec. The canonical coordinator repeats the completed-state/fence
 * proof; native live readiness alone cannot substitute for that proof. */
static int replacement_service_entry(int argc,char **argv){
    if(argc!=6||!migration_path(argv[2])||!token(argv[3],96)||!hex64(argv[4])||!token(argv[5],32)||strlen(argv[5])!=32)return 64;
    int action=-1,origin_proof=!strcmp(argv[1],"replacement-origin-proof"),stop_exec=!strcmp(argv[1],"replacement-service-stop-exec");
    if(!strcmp(argv[1],"replacement-service-status"))action=SC_STATUS;
    if(!strcmp(argv[1],"replacement-service-start"))action=SC_START;
    if(!strcmp(argv[1],"replacement-service-stop"))action=SC_STOP;
    if(!strcmp(argv[1],"replacement-service-restart"))action=SC_RESTART;
    if(!strcmp(argv[1],"replacement-service-current"))action=SC_CURRENT;
    if(action<0&&!origin_proof&&!stop_exec)return 64;
    char state[PATH_MAX],code[PATH_MAX],app[PATH_MAX],guard[PATH_MAX],shell[PATH_MAX],controller[PATH_MAX];
    char global[PATH_MAX],legacy[PATH_MAX],ram[PATH_MAX],path[PATH_MAX*2+80];
    const char *prefix=strcmp(argv[2],"/")?argv[2]:"";
    int base=-1,held=-1,result=75,context=0;
    if(snprintf(state,sizeof state,"%s/opt/var/lib/broray",prefix)>=(int)sizeof state)goto done;
    base=checked_directory(state);if(base<0)goto done;
    held=recovery_inherited_guard(base);
    if(held<0)held=sc_lock_exact_wait(base,"operations.guard",0,action==SC_STATUS?30000:0);
    if(held<0){
        if(action==SC_STATUS&&errno==ETIMEDOUT){close(base);return service_replacement_error("UPDATER_SERVICE_OPERATION_BUSY");}
        goto done;
    }
    context=1;if(rs_context(argv[2],argv[3],argv[5]))goto done;strcpy(sc.migration,argv[4]);if(rs_service_read())goto done;
    char cycle_name[128];int cn=snprintf(cycle_name,sizeof cycle_name,"cycles-%s",sc.origin);
    if(cn<0||cn>=(int)sizeof cycle_name)goto done;int exists=bg_exists(sc.up,cycle_name);if(exists<0)goto done;
    if(action>=0&&(action!=SC_STATUS||exists)){
        sc_close();context=0;result=service_cycle_main(argc,argv,action);close(held);close(base);return result;
    }
#define RSE_PATH(dest,fmt,...) do{int n=snprintf(dest,sizeof dest,fmt,__VA_ARGS__);if(n<0||n>=(int)sizeof dest)goto done;}while(0)
    RSE_PATH(app,"%s/opt/broray",prefix);RSE_PATH(code,"%s/platform-replacement-code/code",sc.oppath);
    RSE_PATH(shell,"%s/opt/bin/ash",prefix);RSE_PATH(guard,"%s/bin/broray-ops-guard",code);
    RSE_PATH(controller,"%s/lib/operation-coordinator.sh",code);
    RSE_PATH(global,"%s/opt/var/lock/broray/global-operation.lock",prefix);RSE_PATH(legacy,"%s/tmp/broray-global-operation.lock",prefix);
    RSE_PATH(ram,"%s/tmp/broray-operations",prefix);RSE_PATH(path,"%s/opt/bin:%s/opt/sbin:/usr/bin:/bin:/usr/sbin:/sbin",prefix,prefix);
#undef RSE_PATH
    if(clearenv())goto done;
    const char *keys[]={"PATH","LC_ALL","BRORAY_ROOT","BRORAY_STATE_ROOT","BRORAY_OPS_CODE_ROOT","BRORAY_OPS_GUARD","BRORAY_OPS_ASH","BRORAY_ROUTES_API_LOCK","BRORAY_OPS_UPDATER_ROOT","BRORAY_LEGACY_GLOBAL_LOCK","BRORAY_OPS_RAM_ROOT","BRORAY_OPS_GUARD_HELD"};
    const char *values[]={path,"C",app,state,code,guard,shell,global,sc.uppath,legacy,ram,"1"};
    for(unsigned i=0;i<sizeof keys/sizeof keys[0];i++)if(setenv(keys[i],values[i],1))goto done;
    int flags=fcntl(sc.guard,F_GETFD);if(flags<0||fcntl(sc.guard,F_SETFD,flags&~FD_CLOEXEC)||chdir("/"))goto done;
    char *command[]={shell,controller,stop_exec?"platform-service-stop":"platform-replacement-public-status",argv[3],argv[4],argv[5],origin_proof?"origin":NULL,NULL};
    execv(shell,command);
done:
    if(context)sc_close();if(held>=0)close(held);if(base>=0)close(base);
    return result?service_replacement_error("REPLACEMENT_SERVICE_ENTRY_UNCONFIRMED"):0;
}

/* Recover bookkeeping ONLY for a witnessed terminal STOPPED from an ended
 * boot. This verifier may differ from the historical runtime: that runtime's
 * private retained bytes, launch closure, READY pin and every ledger witness
 * remain independently authenticated. It never signals, creates a generation,
 * changes an operation/fence, or turns RUNNING/ERROR into STOPPED. */
static int stopped_boot_retirement_main(int argc,char **argv){
    /* ROOT ORIGIN PROOF ORIGIN_NONCE GENERATION STOP_OP STOP_NONCE STATE_SHA */
    if(argc!=10||!migration_path(argv[2])||!token(argv[3],96)||!hex64(argv[4])||
       !token(argv[5],32)||strlen(argv[5])!=32||!token(argv[6],64)||!token(argv[7],96)||
       !token(argv[8],32)||strlen(argv[8])!=32||!hex64(argv[9])||!strcmp(argv[3],argv[7]))return 64;
    int unissued=!strcmp(argv[1],"verify-unissued-stop-boot");
    int result=75,context=0,stop=-1,domain=-1,host=-1,gens=-1,whole=-1,life=-1;
    const char *why="context";char path[PATH_MAX],stop_path[PATH_MAX],fence[PATH_MAX],link[PATH_MAX],field[192],name[128];
    char text[2048],host_text[512],value[128],canonical[PATH_MAX],shell[PATH_MAX],shell_sha[65];
    struct migration_file intent={0},state_file={0},ready={0},last={0},host_record={0},again={0};
    char *copy=NULL,*cursor=NULL,*row=NULL;struct gb_record b;memset(&b,0,sizeof b);
    const char *prefix=!strcmp(argv[2],"/")?"":argv[2];
    if(!realpath(argv[2],canonical)||strcmp(canonical,argv[2]))goto done;
    context=1;if(rs_context(argv[2],argv[3],argv[5]))goto done;sc.may_publish=0;strcpy(sc.migration,argv[4]);
    why="historical-runtime";
    int replacement=bg_exists(sc.op,"platform-replacement-start");if(replacement<0)goto done;
    sc.replacement=replacement;pl_historical_verification=1;
    if(replacement){
        if(sc_file(sc.op,"platform-replacement-start/intent.record",&intent)||strcmp(intent.sha,argv[4]))goto done;
        copy=strdup(intent.bytes);if(!copy)goto done;cursor=copy;row=NULL;
        for(int i=0;i<7;i++){row=sc_line(&cursor);if(!row)goto done;}
        if(!hex64(row))goto done;strcpy(sc.native,row);free(copy);copy=NULL;
        if(rs_service_read())goto done;
    }else{
        /* Clean-install/migration origins have the same sealed ordinary
         * lifecycle, but use the original bootguard and recovery-code proof. */
        if(bg_exists(sc.op,"platform-replacement-service.json")!=0||
           sc_file(sc.op,"platform-bootguard.json",&intent)||
           sc_field(intent.bytes,"nativeSha256",sc.native,sizeof sc.native)||!hex64(sc.native)||
           sc_join(path,sc.oppath,"platform-migration"))goto done;
        int migration=checked_directory(path);if(migration<0)goto done;
        int bad=bg_source(migration,path,sc.root,sc.migration,&sc.input)||
            strcmp(sc.input.operation,sc.origin)||strcmp(sc.input.nonce,sc.nonce);close(migration);
        if(bad)goto done;
        char *verify[]={argv[0],"recovery-code-verify",sc.oppath,sc.root,path,sc.migration,NULL};
        if(recovery_code_impl_native(6,verify,0,sc.native))goto done;
    }
    why="lifecycle-history";
    int k=snprintf(name,sizeof name,replacement?"cycles-%s":"cycles",sc.origin);
    if(k<0||k>=(int)sizeof name||sc_join(sc.cyclepath,sc.uppath,name))goto done;
    sc.cycles=checked_directory(sc.cyclepath);
    why="lifecycle-directory";if(sc.cycles<0)goto done;
    why="lifecycle-lock";if((sc.transition=sc_lock_exact(sc.cycles,"transition.lock",0))<0)goto done;
    why="lifecycle-seal";if(sc_seal_read())goto done;
    why="lifecycle-chain";if(sc_collect())goto done;
    struct sc_node *n=&sc.nodes[sc.current];
    why="generation-or-boot";if(strcmp(n->id,argv[6])||!strcmp(n->born,boot))goto done;
    why="generation-launch";if(sc_load_node(n))goto done;
    why="generation-host";if(sc_host_record(n))goto done;
    why="platform-bytes";if(pl_exact())goto done;
    why="stop-binding";
    if(snprintf(stop_path,sizeof stop_path,"%s/opt/var/lib/broray/operations/%s",prefix,argv[7])>=(int)sizeof stop_path||
       snprintf(path,sizeof path,"%s/opt/var/lock/broray/global-operation.lock",prefix)>=(int)sizeof path||
       sc_join(fence,stop_path,"fence"))goto done;
    ssize_t ln=readlink(path,link,sizeof link);if(ln!=(ssize_t)strlen(fence)||memcmp(link,fence,(size_t)ln))goto done;
    stop=checked_directory(stop_path);if(stop<0||sc_file(stop,"state.json",&state_file)||strcmp(state_file.sha,argv[9]))goto done;
    if(sc_field(state_file.bytes,"operationId",value,sizeof value)||strcmp(value,argv[7])||
       sc_field(state_file.bytes,"operation",value,sizeof value)||strcmp(value,"system:platform-preflight")||
       (!strstr(state_file.bytes,"\"running\":true")&&
        !(unissued&&strstr(state_file.bytes,"\"state\":\"aborted\"")&&strstr(state_file.bytes,"\"errorCode\":\"STOP_INTERRUPTED_BY_REBOOT\"")))||
       !strstr(state_file.bytes,"\"cancelability\":\"protected\""))goto done;
    const char *purpose=strstr(state_file.bytes,"\"serviceStop\":{");if(!purpose)goto done;
    const char *keys[]={"contract","originOperationId","originStopNonce",replacement?"originProofSha256":"originMigrationIntentSha256","generationId","nativeSha256","platformManifestSha256"};
    const char *values[]={replacement?"broray-service-stop/2":"broray-service-stop/1",argv[3],argv[5],argv[4],argv[6],sc.native,sc.input.manifest};
    if(replacement){if(sc_field(purpose,"originKind",value,sizeof value)||strcmp(value,"supervised-replacement"))goto done;}
    else if(strstr(purpose,"\"originKind\":")||strstr(purpose,"\"originProofSha256\":"))goto done;
    for(unsigned i=0;i<sizeof keys/sizeof keys[0];i++)if(sc_field(purpose,keys[i],value,sizeof value)||strcmp(value,values[i]))goto done;
    why="exclusion";
    if(sc_join(path,sc.uppath,"generations"))goto done;gens=checked_directory(path);
    domain=checked_directory(pl.domain);host=checked_directory(pl.host);
    if(gens<0||domain<0||host<0||(whole=sc_lock_exact(gens,".generation-lifetime.lock",0))<0||
       (life=sc_lock_exact(domain,"lifetime.lock",0))<0||flock(host,LOCK_EX|LOCK_NB)||sc_namespace("generations",1))goto done;
    strcpy(b.id,n->id);strcpy(b.manifest,sc.input.manifest);strcpy(b.from,n->born);strcpy(b.through,boot);
    strcpy(b.seal,sc.seal);strcpy(b.host,n->host);strcpy(b.launch,n->launch);strcpy(b.transaction,n->transaction);scope_digest(pl.domain,b.scope);
    k=snprintf(name,sizeof name,"ready-%s.record",n->id);
    if(k<0||k>=(int)sizeof name||sc_file(sc.cycles,name,&ready))goto done;strcpy(b.ready,ready.sha);
    why="witnessed-terminal-ledger";gb_stopped_retirement=!unissued;
    if(gb_measure(domain,pl.domain,&b))goto done;
    record_name(b.total,name);if(sc_file(domain,name,&last)||strcmp(last.sha,b.last))goto done;
    if(unissued){
        /* STOP authorization is durably pinned before any signal. This branch
         * accepts only the earlier boundary: no target binding, no sent STOP,
         * and a fully witnessed RUNNING ledger from a different kernel boot.
         * It grants bookkeeping authority only, never STOPPED or readiness. */
        why="unissued-stop-boundary";
        if(strstr(state_file.bytes,"\"generationStop\":")||
           sc_field(state_file.bytes,"phase",value,sizeof value)||
           (strcmp(value,"working")&&strcmp(value,"finished"))||
           !strstr(state_file.bytes,"\"phase\":\"STOP_INTENT\"")||
           sc_field(state_file.bytes,"stopNonce",value,sizeof value)||strcmp(value,argv[8])||
           !strstr(last.bytes,"\"state\":\"RUNNING\"")||
           !strstr(last.bytes,"\"stopOperationId\":\"\",\"stopNonce\":\"\",\"termSent\":false,"))goto done;
        snprintf(field,sizeof field,"\"nativeSha256\":\"%s\"",sc.native);
        if(!strstr(last.bytes,field)||sc_file(stop,"state.json",&again)||strcmp(again.sha,argv[9])||pl_exact())goto done;
        printf("{\"ok\":true,\"phase\":\"UNISSUED_STOP_BOOT_ENDED_VERIFIED\",\"generationId\":\"%s\",\"oldBootId\":\"%s\",\"currentBootId\":\"%s\",\"ledgerSha256\":\"%s\",\"ledgerRevision\":%lu,\"serviceStopped\":false,\"platformReady\":false,\"signalsAuthorized\":false,\"mutationAuthorized\":false}\n",b.id,b.from,boot,b.last,b.total);
        result=0;goto done;
    }
    if(!strstr(last.bytes,"\"state\":\"STOPPED\"")||!strstr(last.bytes,"\"children\":[]")||
       !strstr(last.bytes,"\"awaitingBirth\":[]")||!strstr(last.bytes,"\"exitedUnreaped\":[]")||
       sc_field(last.bytes,"stopOperationId",value,sizeof value)||strcmp(value,argv[7])||
       sc_field(last.bytes,"stopNonce",value,sizeof value)||strcmp(value,argv[8]))goto done;
    snprintf(field,sizeof field,"\"nativeSha256\":\"%s\"",sc.native);if(!strstr(last.bytes,field))goto done;
    if(!strcmp(argv[1],"verify-stopped-boot")){
        printf("{\"ok\":true,\"phase\":\"STOPPED_RETIREMENT_VERIFIED\",\"generationId\":\"%s\",\"serviceStopped\":true,\"platformReady\":false,\"signalsAuthorized\":false,\"mutationAuthorized\":false}\n",b.id);
        result=0;goto done;
    }
    why="generation-retirement-publication";
    /* The complete historical proof and all exclusion locks precede these
     * two write-once receipts. No other missing evidence may be recreated. */
    sc.may_publish=1;
    struct retired_record r;memset(&r,0,sizeof r);strcpy(r.gen,b.id);strcpy(r.sha,b.manifest);strcpy(r.op,argv[7]);strcpy(r.nonce,argv[8]);
    r.total=b.total;strcpy(r.inventory,b.inventory);strcpy(r.last,b.last);strcpy(r.scope,b.scope);
    k=retirement_text(&r,text,sizeof text);
    if(k<0||k>=(int)sizeof text||sc_publish(domain,"retirement.receipt",text,(size_t)k,1)||retirement_valid(domain,pl.domain,NULL))goto done;
    /* Host verifier independently acquires this lock. Installation exclusion
     * and coordinator guard remain held across the handoff. */
    close(life);life=-1;why="host-retirement-publication";
    if(sc_file(host,"host.record",&host_record)||strcmp(host_record.sha,b.host))goto done;
    copy=strdup(host_record.bytes);if(!copy)goto done;cursor=copy;
    for(int i=0;i<9;i++){
        row=sc_line(&cursor);if(!row)goto done;
        if(i==6){if(!migration_path(row)||strlen(row)>=sizeof shell)goto done;strcpy(shell,row);}
        if(i==7){if(!hex64(row))goto done;strcpy(shell_sha,row);}
    }
    free(copy);copy=NULL;
    char *host_args[]={argv[0],"service-retired",pl.host,pl.domain,b.id,b.manifest,argv[2],shell,shell_sha,b.host,NULL};
    if(service_retirement_text_at(host,host_args,host_record.bytes,host_record.size,host_text,b.from)||
       sc_publish(host,"retirement.receipt",host_text,strlen(host_text),1))goto done;
    char host_receipt[65];
    if(service_host_retired_proof(10,host_args,host,NULL,host_receipt)||sc_file(stop,"state.json",&again)||
       strcmp(again.sha,argv[9])||pl_exact())goto done;
    printf("{\"ok\":true,\"phase\":\"STOPPED_RETIREMENT_RECOVERED\",\"generationId\":\"%s\",\"oldBootId\":\"%s\",\"hostReceiptSha256\":\"%s\",\"serviceStopped\":true,\"platformReady\":false,\"signalsAuthorized\":false}\n",b.id,b.from,host_receipt);
    result=0;
done:
    gb_stopped_retirement=0;pl_historical_verification=0;free(copy);free(intent.bytes);free(state_file.bytes);free(ready.bytes);free(last.bytes);free(host_record.bytes);free(again.bytes);
    if(life>=0)close(life);if(whole>=0)close(whole);if(gens>=0)close(gens);if(host>=0)close(host);if(domain>=0)close(domain);if(stop>=0)close(stop);
    if(context)sc_close();if(result)fprintf(stderr,"STOPPED_BOOT_RECOVERY_FIRST_ERROR=%s\n",why);return result;
}
