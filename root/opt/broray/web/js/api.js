"use strict";

window.BROray = window.BROray || {};

BROray.api = {
    text(value, fallback = "—"){
        if(
            value === undefined ||
            value === null ||
            value === ""
        ){
            return fallback;
        }

        return String(value);
    },

    escapeHtml(value){
        return String(value)
            .replace(/&/g, "&amp;")
            .replace(/</g, "&lt;")
            .replace(/>/g, "&gt;")
            .replace(/"/g, "&quot;")
            .replace(/'/g, "&#039;");
    },

    async requestJson(url, options){
        const response = await fetch(
            url,
            options || {}
        );

        if(!response.ok){
            throw new Error(
                "HTTP " + response.status
            );
        }

        return response.json();
    }
};
