// Draws the paywall for ?country=XX&user_id=... using GET /v1/paywall.
// Text is set with textContent only, never innerHTML.
(function () {
  const params = new URLSearchParams(window.location.search);
  const country = params.get("country") || "";
  const userId = params.get("user_id") || anonymousId();
  const root = document.getElementById("paywall");

  function anonymousId() {
    let id = sessionStorage.getItem("anon_id");
    if (!id) {
      id = "web-" + Math.random().toString(36).slice(2, 12);
      sessionStorage.setItem("anon_id", id);
    }
    return id;
  }

  function el(tag, text, testid, className) {
    const node = document.createElement(tag);
    if (text !== undefined) node.textContent = text;
    if (testid) node.dataset.testid = testid;
    if (className) node.className = className;
    return node;
  }

  function showError(message, code) {
    root.replaceChildren();
    root.dataset.state = "error";
    const p = el("p", message, "error", "error");
    if (code) p.dataset.errorCode = code;
    root.append(p);
  }

  async function api(path, options) {
    const res = await fetch(path, options);
    const body = await res.json().catch(() => ({}));
    return { ok: res.ok, status: res.status, body };
  }

  function render(pw) {
    root.replaceChildren();
    root.dataset.state = "ready";
    root.dataset.tier = pw.tier;
    root.dataset.variant = pw.variant;
    root.dataset.country = pw.country;

    root.append(el("h1", pw.title, "paywall-title"));
    if (pw.trial_days > 0) {
      root.append(el("p", pw.trial_days + "-day free trial. Cancel anytime.", "trial-text", "trial"));
    }

    let selected = (pw.products.find((p) => p.highlighted) || pw.products[0]).product_id;
    const list = el("div", undefined, "products", "products");
    list.setAttribute("role", "radiogroup");
    const buttons = [];

    pw.products.forEach((p) => {
      const b = el("button", undefined, "product-" + p.product_id, "product");
      b.type = "button";
      b.setAttribute("role", "radio");
      b.append(el("span", p.title));
      b.append(el("span", p.display_price + " / " + p.period, "price-" + p.product_id, "price"));
      if (p.highlighted) b.append(el("span", "Best value", "badge-" + p.product_id, "badge"));
      b.addEventListener("click", () => { selected = p.product_id; sync(); });
      buttons.push([p.product_id, b]);
      list.append(b);
    });

    function sync() {
      buttons.forEach(([id, b]) => b.setAttribute("aria-checked", String(id === selected)));
    }
    sync();
    root.append(list);

    const cta = el("button", pw.cta_text, "cta");
    cta.id = "cta";
    cta.type = "button";
    root.append(cta);

    const restore = el("button", "Restore purchases", "restore", "link");
    restore.type = "button";
    root.append(restore);

    const status = el("p", "", "status", "status");
    status.setAttribute("role", "status");
    root.append(status);

    root.append(el("p", "Region tier: " + pw.tier + " | Variant: " + pw.variant, "meta", "meta"));

    const post = (path, payload) => api(path, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload),
    });

    cta.addEventListener("click", async () => {
      cta.disabled = true;
      const r = await post("/v1/purchases", {
        user_id: userId, product_id: selected, store: "app_store",
        country: pw.country, sandbox: true, start_trial: pw.trial_days > 0,
      });
      if (r.ok) {
        status.textContent = "Subscribed (" + r.body.subscription.state + ")";
        status.dataset.result = "subscribed";
      } else {
        status.textContent = r.body.error ? r.body.error.message : "Purchase failed";
        status.dataset.result = r.body.error ? r.body.error.code : "error";
        cta.disabled = false;
      }
    });

    restore.addEventListener("click", async () => {
      const r = await post("/v1/restore", { user_id: userId, store: "app_store" });
      const found = r.ok && r.body.restored;
      status.textContent = found ? "Purchases restored" : "No purchases found";
      status.dataset.result = found ? "restored" : "nothing_to_restore";
    });
  }

  async function load() {
    const qs = new URLSearchParams({ country: country, user_id: userId });
    try {
      const r = await api("/v1/paywall?" + qs);
      if (!r.ok) {
        showError(r.body.error ? r.body.error.message : "Could not load the paywall.", r.body.error && r.body.error.code);
        return;
      }
      render(r.body);
    } catch (e) {
      showError("Could not load the paywall.", "network");
    }
  }

  load();
})();
