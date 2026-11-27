"use strict";
(() => {
  const $ = (id) => document.getElementById(id);
  const text = (id, value) => {
    $(id).textContent = value == null ? "" : String(value);
  };
  const val = (id) => $(id).value;
  let state = {},
    resultPage = 0,
    nodePage = 0,
    pending = 0,
    pollInFlight = false,
    lastRevision,
    requestSequence = 0,
    appliedSequence = 0;
  const pageSize = 50;
  const stateNames = {
    connected: "已连接",
    disconnected: "未连接",
    connecting: "连接中",
    reconnecting: "重连中",
    disconnecting: "断开中",
  };
  function notice(message) {
    text("notice", message);
    $("notice").hidden = !message;
  }
  function errorText(error) {
    return error instanceof Error ? error.message : String(error);
  }
  async function api(path, body, quiet = false) {
    const sequence = ++requestSequence;
    if (!quiet) {
      pending++;
      document.body.setAttribute("aria-busy", "true");
    }
    try {
      const response = await fetch(
        "/api/" + path,
        body === undefined
          ? { cache: "no-store" }
          : {
              method: "POST",
              headers: { "Content-Type": "application/json" },
              body: JSON.stringify(body),
            },
      );
      let data;
      try {
        data = await response.json();
      } catch {
        throw new Error(
          "服务返回了无法解析的响应（HTTP " + response.status + "）",
        );
      }
      if (
        data &&
        data.state &&
        typeof data.state === "object" &&
        sequence >= appliedSequence
      ) {
        appliedSequence = sequence;
        applyState(data.state);
      }
      if (!response.ok || data.error)
        throw new Error(
          data.error || "请求失败（HTTP " + response.status + "）",
        );
      if (!quiet) notice("");
      return data;
    } catch (error) {
      if (!quiet) notice(errorText(error));
      throw error;
    } finally {
      if (!quiet && --pending === 0) document.body.removeAttribute("aria-busy");
    }
  }
  async function action(path, body) {
    try {
      return await api(path, body);
    } catch {
      return null;
    }
  }
  function applyState(next) {
    state = next;
    const connected = state.state === "connected";
    const label = stateNames[state.state] || state.state || "未连接";
    text("top-status", label);
    text("connection-state", label);
    text("metric-state", label);
    $("top-dot").classList.toggle("connected", connected);
    $("connection-state").classList.toggle("connected", connected);
    text("metric-target", state.target || "等待连接");
    text("demo-target", state.demo_target || "");
    $("demo-start").hidden = !!state.demo_target;
    $("demo-stop").hidden = !state.demo_target;
    text("metric-count", (state.results || []).length + " 个");
    const results = state.results || [];
    const good = results.filter(
      (r) => String(r.quality).toUpperCase() === "GOOD",
    ).length;
    text("metric-good", results.length ? good + " / " + results.length : "—");
    text("metric-polling", state.polling ? "运行中" : "未开启");
    text("poll-indicator", state.polling ? "运行中" : "已关闭");
    text(
      "record-status",
      state.record_error
        ? "记录异常"
        : state.record_dir
          ? "记录已启用"
          : "记录状态待确认",
    );
    text(
      "record-path",
      state.record_error
        ? "记录异常：" + state.record_error
        : state.record_dir
          ? "记录目录：" + state.record_dir
          : "",
    );
    text("result-badge", results.length);
    text(
      "scan-status",
      state.scan_status || "读取或扫描后，结果将显示在这里。",
    );
    text(
      "browse-status",
      state.browse_status ||
        "从 Objects 节点开始浏览。选择节点可加入读取列表。",
    );
    const wr = state.write_result;
    text(
      "write-result",
      wr
        ? "写入结果：" +
            (wr.state || "—") +
            (wr.error ? " · " + wr.error : "") +
            (state.readback ? " · 回读：" + state.readback : "")
        : state.readback
          ? "回读：" + state.readback
          : "",
    );
    if (state.last_error) notice(state.last_error);
    // Snapshots can be large: rebuild only when the service revision changes.
    if (next.revision === undefined || next.revision !== lastRevision) {
      lastRevision = next.revision;
      renderResults();
      renderNodes();
      renderLogs();
    }
  }
  function displayValue(v) {
    if (!v) return "—";
    if (Array.isArray(v.elements))
      return (
        "[" +
        v.elements
          .map((element) => {
            const value = displayValue(element);
            return ["String", "DateTime", "ByteString"].includes(
              element.type,
            ) && !Array.isArray(element.elements)
              ? JSON.stringify(value)
              : value;
          })
          .join(", ") +
        "]"
      );
    if (v.type === "Null") return "null";
    if (v.type === "ByteString") {
      if (v.raw == null) return "";
      try {
        return Array.from(atob(v.raw), (c) =>
          c.charCodeAt(0).toString(16).padStart(2, "0"),
        ).join("");
      } catch {
        return "无效的字节串数据";
      }
    }
    for (const key of ["integer", "boolean", "real", "text", "value"])
      if (Object.prototype.hasOwnProperty.call(v, key)) return String(v[key]);
    return "—";
  }
  function formatTime(time) {
    if (!time || String(time).startsWith("0001-")) return "—";
    const d = new Date(time);
    return Number.isNaN(d.getTime())
      ? String(time)
      : d.toLocaleString("zh-CN", { hour12: false });
  }
  function make(tag, content, className) {
    const el = document.createElement(tag);
    if (content != null) el.textContent = String(content);
    if (className) el.className = className;
    return el;
  }
  function activateTab(name) {
    document.querySelectorAll("[role=tab]").forEach((tab) => {
      const active = tab.dataset.tab === name;
      tab.setAttribute("aria-selected", String(active));
      tab.tabIndex = active ? 0 : -1;
      $("panel-" + tab.dataset.tab).hidden = !active;
    });
  }
  document.querySelectorAll("[role=tab]").forEach((tab, index, tabs) => {
    tab.addEventListener("click", () => activateTab(tab.dataset.tab));
    tab.addEventListener("keydown", (event) => {
      if (!["ArrowRight", "ArrowLeft", "Home", "End"].includes(event.key))
        return;
      event.preventDefault();
      let next =
        event.key === "Home"
          ? 0
          : event.key === "End"
            ? tabs.length - 1
            : (index + (event.key === "ArrowRight" ? 1 : tabs.length - 1)) %
              tabs.length;
      tabs[next].focus();
      activateTab(tabs[next].dataset.tab);
    });
  });
  function renderResults() {
    const query = val("filter").toLocaleLowerCase();
    const items = (state.results || []).filter(
      (r) =>
        !query ||
        [
          r.address?.node_id,
          r.display_name,
          r.quality,
          r.data_type,
          displayValue(r.value),
          r.error,
        ].some((x) =>
          String(x || "")
            .toLocaleLowerCase()
            .includes(query),
        ),
    );
    const pages = Math.max(1, Math.ceil(items.length / pageSize));
    resultPage = Math.min(resultPage, pages - 1);
    const tbody = $("results-body");
    tbody.replaceChildren();
    for (const r of items.slice(
      resultPage * pageSize,
      (resultPage + 1) * pageSize,
    )) {
      const row = make("tr"),
        address = make("td"),
        id = r.address?.node_id || "";
      address.append(make("strong", r.display_name || id || "—"));
      if (r.display_name && id) address.append(make("small", id));
      const value = make("td", displayValue(r.value));
      if (r.error) value.append(make("small", r.error));
      const quality = make("span", r.quality || "UNKNOWN", "quality");
      const q = String(r.quality || "").toUpperCase();
      if (q !== "GOOD") quality.classList.add(q === "BAD" ? "bad" : "unknown");
      const qualityCell = make("td");
      qualityCell.append(quality);
      const control = make("td"),
        select = make("button", "选用", "row-action");
      select.type = "button";
      select.setAttribute("aria-label", "选用 " + id);
      select.addEventListener("click", () => {
        $("write-node").value = id;
        const type = r.value?.type || r.data_type;
        if ([...$("write-type").options].some((o) => o.value === type))
          $("write-type").value = type;
        updateWriteHint();
        $("write-node").focus();
      });
      control.append(select);
      row.append(
        address,
        value,
        make("td", r.value?.type || r.data_type || "—"),
        qualityCell,
        make(
          "td",
          formatTime(
            r.source_time && !String(r.source_time).startsWith("0001-")
              ? r.source_time
              : r.time,
          ),
        ),
        control,
      );
      tbody.append(row);
    }
    $("results-empty").hidden = items.length > 0;
    $("results-empty").querySelector("strong").textContent = query
      ? "没有匹配的点位"
      : "还没有读取数据";
    $("results-empty").querySelector("p").textContent = query
      ? "试试其他搜索词。"
      : "选择节点或在右侧输入 NodeID，开始读取。";
    text(
      "result-range",
      items.length
        ? `${resultPage * pageSize + 1}–${Math.min((resultPage + 1) * pageSize, items.length)} / ${items.length} 个点位`
        : "0 个点位",
    );
    text("page-label", `${resultPage + 1} / ${pages}`);
    $("prev-page").disabled = resultPage === 0;
    $("next-page").disabled = resultPage >= pages - 1;
  }
  function appendNode(id) {
    if (!id) return;
    const existing = val("read-nodes");
    $("read-nodes").value = existing
      ? existing + (existing.endsWith("\n") ? "" : "\n") + id
      : id;
  }
  function renderNodes() {
    const items = state.nodes || [],
      pages = Math.max(1, Math.ceil(items.length / pageSize));
    nodePage = Math.min(nodePage, pages - 1);
    const list = $("nodes-list");
    list.replaceChildren();
    for (const n of items.slice(
      nodePage * pageSize,
      (nodePage + 1) * pageSize,
    )) {
      const row = make("div", null, "node-row"),
        info = make("div", null, "node-info");
      info.append(
        make("strong", n.display_name || n.browse_name || n.node_id || "—"),
        make("small", n.node_id || ""),
      );
      const browse = make("button", "浏览", "row-action"),
        add = make("button", "＋ 读取", "row-action");
      browse.type = add.type = "button";
      browse.setAttribute("aria-label", "浏览 " + n.node_id);
      add.setAttribute("aria-label", "加入读取 " + n.node_id);
      browse.addEventListener("click", () => {
        $("browse-node").value = n.node_id;
        browseNode();
      });
      add.addEventListener("click", () => {
        appendNode(n.node_id);
        text("browse-status", "已加入读取列表：" + n.node_id);
      });
      row.append(info, make("span", n.class || "", "node-class"), browse, add);
      list.append(row);
    }
    $("nodes-empty").hidden = items.length > 0;
    text(
      "node-range",
      items.length
        ? `${nodePage * pageSize + 1}–${Math.min((nodePage + 1) * pageSize, items.length)} / ${items.length} 个节点`
        : "0 个节点",
    );
    text("node-page", `${nodePage + 1} / ${pages}`);
    $("node-prev").disabled = nodePage === 0;
    $("node-next").disabled = nodePage >= pages - 1;
  }
  function renderLogs() {
    const logs = state.logs || [];
    const container = $("logs");
    container.replaceChildren();
    for (const item of logs.slice(-500).reverse())
      container.append(
        make(
          "div",
          typeof item === "string" ? item : JSON.stringify(item),
          "log-line",
        ),
      );
    $("logs-empty").hidden = logs.length > 0;
  }
  async function browseNode() {
    nodePage = 0;
    await action("browse", { node: val("browse-node") });
  }
  $("browse").addEventListener("click", browseNode);
  $("discover").addEventListener("click", async () => {
    const data = await action("endpoints", { target: val("target") });
    if (data) {
      const box = $("endpoint-results");
      box.hidden = false;
      box.textContent = data.endpoints?.length
        ? JSON.stringify(data.endpoints, null, 2)
        : "未发现端点。";
    }
  });
  $("connect").addEventListener("click", async () => {
    const result = await action("connect", {
      target: val("target"),
      username: val("username"),
      password: val("password"),
      policy: val("policy"),
      mode: val("mode"),
      certificate: val("certificate"),
      private_key: val("private-key"),
      server_certificate: val("server-certificate"),
      auto_reconnect: $("auto-reconnect").checked,
    });
    if (result) $("password").value = "";
  });
  $("disconnect").addEventListener("click", () => action("disconnect", {}));
  $("demo-start").addEventListener("click", async () => {
    const data = await action("demo/start", {});
    if (data?.state?.demo_target) $("target").value = data.state.demo_target;
  });
  $("demo-stop").addEventListener("click", () => action("demo/stop", {}));
  $("read").addEventListener("click", async () => {
    const nodes = val("read-nodes")
      .split(/\r?\n/)
      .filter((line) => line.trim() !== "");
    if (!nodes.length) return notice("请输入至少一个 NodeID。");
    const data = await action("read", { nodes });
    if (data) activateTab("results");
  });
  $("read-all").addEventListener("click", async () => {
    const data = await action("read-all", { node: val("browse-node") });
    if (data) activateTab("results");
  });
  $("refresh-start").addEventListener("click", () => {
    const interval = Number(val("interval"));
    if (!Number.isInteger(interval) || interval < 100 || interval > 86400000)
      return notice("刷新间隔须为 100 到 86400000 毫秒。");
    action("refresh/start", { interval_ms: interval });
  });
  $("refresh-stop").addEventListener("click", () => action("refresh/stop", {}));
  function updateWriteHint() {
    text(
      "write-hint",
      val("write-type") === "ByteString"
        ? "字节串使用十六进制（如 0a1b）；数组使用 JSON 字符串数组。"
        : "数组使用 JSON，例如 [1,2,3]。",
    );
  }
  $("write-type").addEventListener("change", updateWriteHint);
  $("write-cancel").addEventListener("click", () =>
    $("write-dialog").close("cancel"),
  );
  $("write-confirm").addEventListener("click", () =>
    $("write-dialog").close("confirm"),
  );
  document
    .querySelector('a[href="#activity"]')
    .addEventListener("click", () => activateTab("logs"));
  $("write").addEventListener("click", () => {
    if (!val("write-node").trim()) return notice("请输入目标 NodeID。");
    if (state.state !== "connected" || state.generation == null)
      return notice("请先连接服务器，再确认写入。");
    const count = Number(val("write-count"));
    if (!Number.isInteger(count) || count < 1 || count > 65535)
      return notice("写入数量须为 1 到 65535 的整数。");
    const payload = {
      node: val("write-node"),
      type: val("write-type"),
      count,
      value: val("write-value"),
      generation: String(state.generation),
      approved: true,
    };
    const summary = $("write-summary");
    summary.replaceChildren();
    for (const [key, value] of [
      ["服务器", state.target],
      ["NodeID", payload.node],
      ["类型 / 数量", payload.type + " / " + payload.count],
      [
        payload.type === "ByteString" ? "写入值（十六进制）" : "写入值",
        payload.value,
      ],
      ["连接代次", payload.generation],
    ])
      summary.append(make("dt", key), make("dd", value));
    const dialog = $("write-dialog");
    dialog.returnValue = "";
    dialog.showModal();
    dialog.addEventListener(
      "close",
      async function confirmWrite() {
        if (dialog.returnValue !== "confirm") return;
        if (
          String(state.generation) !== payload.generation ||
          state.state !== "connected"
        )
          return notice("连接已变化，请重新检查并确认写入。");
        await action("write", payload);
      },
      { once: true },
    );
  });
  $("filter").addEventListener("input", () => {
    resultPage = 0;
    renderResults();
  });
  $("prev-page").addEventListener("click", () => {
    resultPage--;
    renderResults();
  });
  $("next-page").addEventListener("click", () => {
    resultPage++;
    renderResults();
  });
  $("node-prev").addEventListener("click", () => {
    nodePage--;
    renderNodes();
  });
  $("node-next").addEventListener("click", () => {
    nodePage++;
    renderNodes();
  });
  async function poll() {
    if (pollInFlight || pending) return;
    pollInFlight = true;
    try {
      await api("state", undefined, true);
    } catch (error) {
      notice("无法获取服务状态：" + errorText(error));
    } finally {
      pollInFlight = false;
    }
  }
  updateWriteHint();
  activateTab("results");
  poll();
  setInterval(poll, 2000);
})();
