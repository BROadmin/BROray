"use strict";

window.BROray = window.BROray || {};

BROray.app = {
    refreshTimer: null,

    init(){
        BROray.dashboard.refresh();

        if(this.refreshTimer !== null){
            clearInterval(this.refreshTimer);
        }

        this.refreshTimer = setInterval(function(){
            if(
                BROray.state.currentPage === "home"
            ){
                BROray.dashboard.refresh();
            }
        }, 3000);
    }
};

if(document.readyState === "loading"){
    document.addEventListener(
        "DOMContentLoaded",
        function(){
            BROray.app.init();
        },
        { once: true }
    );
}else{
    BROray.app.init();
}
