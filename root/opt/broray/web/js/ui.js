"use strict";

window.BROray = window.BROray || {};

BROray.state = BROray.state || {
    currentPage: "home",
    refreshBusy: false,
    subscriptionsLoaded: false,
    xrayLoaded: false
};

if(
    BROray.state.xrayLoaded === undefined
){
    BROray.state.xrayLoaded = false;
}

BROray.ui = {
    showPage(page){
        const pageElement =
            document.getElementById("page-" + page);

        const navElement =
            document.getElementById("nav-" + page);

        if(!pageElement){
            console.error(
                "Страница не найдена:",
                page
            );
            return;
        }

        BROray.state.currentPage = page;

        document
            .querySelectorAll(".page")
            .forEach(function(element){
                element.classList.remove("active");
            });

        document
            .querySelectorAll(".nav-button")
            .forEach(function(element){
                element.classList.remove("active");
            });

        pageElement.classList.add("active");

        if(navElement){
            navElement.classList.add("active");
        }

        if(
            page === "subscriptions" &&
            !BROray.state.subscriptionsLoaded
        ){
            BROray.subscriptions.load();
        }

        if(
            page === "xray" &&
            !BROray.state.xrayLoaded &&
            BROray.xray
        ){
            BROray.xray.load();
        }

        window.scrollTo({
            top: 0,
            behavior: "smooth"
        });
    }
};

window.showPage = function(page){
    BROray.ui.showPage(page);
};
