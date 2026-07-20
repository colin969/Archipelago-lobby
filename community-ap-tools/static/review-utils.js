let _toastTimeout = null;
function showToast(msg, type = "error") {
    let el = document.getElementById("toast");
    if (!el) {
        el = document.createElement("div");
        el.id = "toast";
        document.body.appendChild(el);
    }
    el.className = `toast ${type}`;
    el.textContent = msg;
    el.classList.add("visible");
    clearTimeout(_toastTimeout);
    _toastTimeout = setTimeout(() => el.classList.remove("visible"), 4000);
}

function h(tag, attrs, ...children) {
    const el = document.createElement(tag);
    for (const [k, v] of Object.entries(attrs || {})) {
        if (v == null || v === false) continue;
        if (k === "className") el.className = v;
        else if (k.startsWith("on") || k === "value" || k === "selected" || k === "disabled" || k === "checked") el[k] = v;
        else el.setAttribute(k, String(v));
    }
    for (const c of children) if (c != null && c !== false) el.append(c);
    return el;
}

function field(label, input) {
    return h("div", { className: "field" }, h("span", null, label), input);
}

function selectEl(className, options, selected) {
    return h("select", { className }, ...options.map(([val, text]) =>
        h("option", { value: val, selected: val === selected }, text)
    ));
}

function confirmDelete(name, callback) {
    const cancelBtn = h("button", { className: "small", onclick: () => dialog.remove() }, "Close");
    const deleteBtn = h("button", { className: "small danger", onclick: () => { dialog.remove(); callback(); } }, "Yes, delete it");
    const dialog = h("dialog", { className: "delete-popup" },
        h("span", { className: "popup-title" }, "Are you sure?"),
        h("div", { className: "popup-content" }, `Are you sure you want to delete "${name}"?`),
        h("div", { className: "popup-buttons" }, cancelBtn, deleteBtn),
    );
    dialog.onclick = (e) => { if (e.target === dialog) dialog.remove(); };
    document.body.appendChild(dialog);
    dialog.showModal();
}

function createTrackerTable(tableId)
{
    const statusFormatter = function (cell, formatterParams) {
        const value = cell.getValue();
        return `<div class="slot-status slot-status-${value}">${value.charAt(0)}</span>`;
    }

    const checksFormatter = function (cell, formatterParams) {
        const values = cell.getValue();
        return `${values[0]} / ${values[1]}`;
    }

    const checksCalc = function (values, data, calcParams) {
        let totalFound = 0;
        let totalExisting = 0;

        for (const value of values) {
            totalFound += value[0];
            totalExisting += value[1];
        }

        return [totalFound, totalExisting];
    }

    const checksCalcFormatter = function (cell, formatterParams) {
        const values = cell.getValue();
        return `${values[0]} / ${values[1]}`;
    }

    const checksPercentFormatter = function (cell, formatterParams) {
        const values = cell.getValue();
        const percent = ((values[0] / values[1]) * 100).toFixed(1);
        return `${percent}%`;
    }

    const lastActivityFormatter = function (cell, formatterParams) {
        const value = cell.getValue();
        if (value === null)
        {
            return "Never";
        }
        return value;
    }

    const onDiscordHandleClick = function (event, cell) {
        const row = cell.getRow();
        const discordId = row.getData().discord_id;

        navigator.clipboard.writeText(`<@${discordId}`);
    }

    const table = new Tabulator(tableId, {
        ajaxURL: "/api/tracker_info",
        height: "400px",
        layout: "fitDataStretch",
        rowContextMenu: [
            {
                label: "Password",
                action: function (event, row) {
                    const { lobby_slot_id, name, password } = row.getData();
                    const pwd = password !== null ? `"${password}"` : "null";
                    openPasswordPopup(lobby_slot_id, name, pwd);
                }
            },
            {
                label: "Change Owner",
                action: function (event, row) {
                    const { lobby_slot_id, name, password } = row.getData();
                    const pwd = password !== null ? `"${password}"` : "null";
                    openChangeOwnerPopup(lobby_slot_id, name, pwd);
                }
            },
            {
                label: "Release Slot",
                action: function (event, row) {
                    const { name, game } = row.getData();
                    openRelease(name, game);
                }
            },
            {
                label: "Hint Item",
                action: function (event, row) {
                    const { name, game } = row.getData();
                    openAction("hint", name, game, "item");
                }
            },
            {
                label: "Give Item",
                action: function (event, row) {
                    const { name, game } = row.getData();
                    openAction("give", name, game, "item");
                }
            },
            {
                label: "Hint Location",
                action: function (event, row) {
                    const { name, game } = row.getData();
                    openAction("hint", name, game, "location");
                }
            },
            {
                label: "Give Location",
                action: function (event, row) {
                    const { name, game } = row.getData();
                    openAction("give", name, game, "location");
                }
            }
        ],
        columns: [
            { title: "S", field: "status", hozAlign: "center", formatter: statusFormatter},
            { title: "Name", field: "name", headerFilter: "input" },
            { title: "Game", field: "game", headerFilter:"list", headerFilterParams: { valuesLookup:true, clearable:true, sort: "asc" } },
            { title: "Checks", field: "checks", formatter: checksFormatter, bottomCalc: checksCalc, bottomCalcFormatter: checksCalcFormatter},
            { title: "Percent", field: "checks", formatter: checksPercentFormatter, bottomCalc: checksCalc, bottomCalcFormatter: checksPercentFormatter },
            { title: "Last Active", field: "last_activity", formatter: lastActivityFormatter },
            { title: "Discord Handle", field: "discord_handle", cellClick: onDiscordHandleClick, headerFilter: "input" }
        ]
    });
}