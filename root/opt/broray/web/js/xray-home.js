"use strict";

window.BROray = window.BROray || {};

BROray.xrayHome = {
    ensureCard(){
        const page =
            document.getElementById(
                "page-home"
            );

        if(!page){
            return null;
        }

        let card =
            document.getElementById(
                "home-xray-card"
            );

        if(!card){
            card =
                document.createElement(
                    "div"
                );

            card.id =
                "home-xray-card";

            card.className =
                "card home-xray-compact-card";
        }

        if(page.firstElementChild !== card){
            page.insertBefore(
                card,
                page.firstElementChild
            );
        }

        return card;
    },

    versionNumber(value){
        const text =
            String(value || "");

        const match =
            text.match(
                /Xray\s+([0-9]+(?:\.[0-9]+)+)/
            );

        if(match && match[1]){
            return match[1];
        }

        return "—";
    },

    draw(running, version){
        const card =
            this.ensureCard();

        if(!card){
            return;
        }

        card.innerHTML =
            '<div class="home-xray-compact-row">' +
                '<div class="home-xray-compact-state">' +
                    '<span class="' +
                        'home-xray-compact-dot ' +
                        (
                            running
                                ? "is-running"
                                : "is-stopped"
                        ) +
                    '">' +
                    '</span>' +
                    '<span class="' +
                        'home-xray-compact-title">' +
                        'Xray ' +
                        BROray.api.escapeHtml(
                            version
                        ) +
                    '</span>' +
                '</div>' +
                '<button ' +
                    'type="button" ' +
                    'class="' +
                        'small-button ' +
                        'home-xray-details-button" ' +
                    'onclick="showPage(\'xray\')"' +
                '>' +
                    'Подробнее' +
                '</button>' +
            '</div>';
    },

    render(result){
        const status =
            result.status || {};

        this.draw(
            status.running === true,
            this.versionNumber(
                status.version
            )
        );
    },

    renderError(){
        this.draw(
            false,
            "—"
        );
    },

    async refresh(){
        this.ensureCard();

        try{
            const result =
                await BROray.api.requestJson(
                    "cgi-bin/xray.cgi?" +
                    Date.now()
                );

            if(result.ok){
                this.render(result);
            }else{
                this.renderError();
            }
        }catch(error){
            this.renderError();
        }
    }
};

document.addEventListener(
    "DOMContentLoaded",
    function(){
        BROray.xrayHome.refresh();

        window.setInterval(
            function(){
                BROray.xrayHome.refresh();
            },
            30000
        );
    }
);
