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
        return `<div class="slot-status slot-status-${value.replace(/\s/g, "")}"></span>`;
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

    const checksSorter = function (a, b) {
        return a[1] - b[1];
    }

    const checksPercentSorter = function (a, b) {
        return (a[0] / a[1]) - (b[0] / b[1]);
    }

    const lastActivityFormatter = function (cell, formatterParams) {
        const value = cell.getValue();
        const row = cell.getRow();
        const goaled = row.getData().status === "GoalCompleted";

        if (value === null)
        {
            return "Never";
        }
        let timeSinceClassname = "last-active-recent";
        if (!goaled) {
            if (value >= 3600) {
                timeSinceClassname = "last-active-hardbk";
            } else if (value >= 1800) {
                timeSinceClassname = "last-active-softbk";
            }
        }
        
        const text = timeSince(value) + " ago"
        return `<div class="${timeSinceClassname}">${text}</div>`;
    }

    const lastActivitySorter = function (a, b, aRow, bRow) {
        const aGoaled = aRow.getData().status === "GoalCompleted";
        const bGoaled = bRow.getData().status === "GoalCompleted";

        if (aGoaled !== bGoaled) {
            return aGoaled ? 1 : -1;
        }

        return a - b;
    }

    const onDiscordHandleClick = function (event, cell) {
        const row = cell.getRow();
        const discordId = row.getData().discord_id;

        navigator.clipboard.writeText(`<@${discordId}>`);
    }

    const table = new Tabulator(tableId, {
        ajaxURL: "/api/tracker_info",
        height: "100%",
        layout: "fitDataStretch",
        persistence: true,
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
                label: "Toggle DeathBlock",
                action: function (event, row) {
                    const { name, game, deathlink_excluded } = row.getData();
                    openDeathBlock(name, game, deathlink_excluded);
                }
            },
            {
                label: "Goal Slot",
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
        initialSort: [
            { column: "Name", dir:"asc" }
        ],
        columns: [
            { title: "S", field: "status", hozAlign: "center", formatter: statusFormatter },
            { title: "Name", field: "name", headerFilter: "input" },
            { title: "Game", field: "game", headerFilter:"list", headerFilterParams: { valuesLookup:true, clearable:true, sort: "asc" } },
            { title: "Checks", field: "checks", formatter: checksFormatter, sorter: checksSorter, bottomCalc: checksCalc, bottomCalcFormatter: checksCalcFormatter},
            { title: "Percent", field: "percent", mutator: function (value, data) {
                return data.checks;
            }, formatter: checksPercentFormatter, sorter: checksPercentSorter, bottomCalc: checksCalc, bottomCalcFormatter: checksPercentFormatter },
            { title: "Last Active", field: "last_activity", formatter: lastActivityFormatter, sorter: lastActivitySorter },
            { title: "Discord Handle", field: "discord_handle", cellClick: onDiscordHandleClick, headerFilter: "input" },
            { title: "Deaths Allowed", field: "death_allowed", mutator: function (value, data) {
                return !data.deathlink_excluded;
            }, hozAlign: "center", formatter: "tickCross" },
            { title: "Deaths", field: "deathlinks_sent", bottomCalc: "sum" },
        ]
    });

    table.on("dataLoaded", function (data) {
        console.log("data loaded");
        // Really shouldn't be using globals here, but eh
        window.review_data = data;
        if (window.slots_loaded_once !== true)
        {
            refreshSlotsToPing();
        }
    });

    window.review_table = table;

    setInterval(() => {
        table.replaceData("/api/tracker_info");
    }, 30000);
}

function forceReviewTableRefresh() {
    if (window.review_table) {
        window.review_table.replaceData("/api/tracker_info");
    }
}

function refreshSlotsToPing() {
    if (window.review_data !== undefined)
    {
        window.slots_loaded_once = true;
        // Get list of users with no activity
        const seen = new Set();
        const neverConnected = [];
        for (const row of window.review_data) {
            if (row["last_active"] == null && row["status"] == "Disconnected" && !seen.has(row["discord_id"])) {
                seen.add(row["discord_id"]);
                neverConnected.push([row["discord_handle"], row["discord_id"]]);
            }
        }

        const container = document.getElementById("unconnected-slots");

        // Remove old list
        container.querySelectorAll("ul").forEach(ul => ul.remove());

        // Build new list
        const ul = document.createElement("ul");

        // Chunk into groups
        for (let i = 0; i < neverConnected.length; i += 10) {
            const chunk = neverConnected.slice(i, i + 10);
            const mentions = chunk.map((val) => `<@${val[1]}>`).join(" ");
            const li = document.createElement("li");
    
            for (const val of chunk) {
                const span = document.createElement("span");
                span.textContent = "@" + val[0];
                li.appendChild(span);
            }
    
            const button = document.createElement("button");
            button.style.marginLeft = "10px";
            button.style.cursor = "pointer";
            button.innerHTML = '<i class="fa-solid fa-copy"></i>';
            button.onclick = function () {
                navigator.clipboard.writeText(mentions + " you are not connected. If you need help please speak in the AP support channel. If you are connected in the meantime all good, you can ignore the ping");
            };
            li.appendChild(button);
            ul.appendChild(li);
        }
    
        container.appendChild(ul);
    }
}

function timeSince(secondsSince) {
    const seconds = Math.floor(secondsSince);
  
    let interval = seconds / 31536000;
  
    if (interval > 1) {
      return Math.floor(interval) + " years";
    }
    interval = seconds / 2592000;
    if (interval > 1) {
      return Math.floor(interval) + " months";
    }
    interval = seconds / 86400;
    if (interval > 1) {
      return Math.floor(interval) + " days";
    }
    interval = seconds / 3600;
    if (interval > 1) {
      return Math.floor(interval) + " hours";
    }
    interval = seconds / 60;
    if (interval > 1) {
      return Math.floor(interval) + " minutes";
    }
    return Math.floor(seconds) + " seconds";
}