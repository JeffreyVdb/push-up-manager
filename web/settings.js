/* A list/detail settings window; compact screens navigate one level at a time. */
"use strict";
(() => {
  const dialog = document.getElementById("settings");
  const title = document.getElementById("settings-title");
  const back = document.getElementById("settings-back");
  const content = dialog.querySelector(".settings-content");
  const wide = window.matchMedia("(min-width: 48rem)");
  const destinations = Array.from(dialog.querySelectorAll(".settings-nav a"), link => ({
    link, section: document.getElementById(link.hash.slice(1)), label: link.querySelector("strong").textContent,
  }));
  let current = null;
  let session = null;
  let closing = false;

  function entry() {
    const value = history.state?.pushupSettings;
    return value?.session === session ? value : null;
  }

  function render(id, focus = false, preserveScroll = false) {
    const previous = current;
    const scrollTop = content.scrollTop;
    current = destinations.find(({ section }) => section.id === id && !section.hidden) || null;
    if (!current && wide.matches) current = destinations[0];
    for (const { link, section } of destinations) {
      link.hidden = section.hidden;
      section.toggleAttribute("data-active", section === current?.section);
      if (section === current?.section) link.setAttribute("aria-current", "page");
      else link.removeAttribute("aria-current");
    }
    if (current) dialog.dataset.section = current.section.id;
    else delete dialog.dataset.section;
    title.textContent = !wide.matches && current ? current.label : "Settings";
    back.hidden = wide.matches || !current;
    content.scrollTop = preserveScroll ? scrollTop : 0;
    if (focus && dialog.open) {
      const target = current
        ? (wide.matches ? current.section.querySelector("h3") : title)
        : (previous?.link.hidden === false ? previous.link : destinations[0].link);
      target.focus({ preventScroll: true });
    }
  }

  function navigate(id) {
    const state = entry();
    if (!state) return;
    const next = { ...state, section: id, depth: wide.matches ? state.depth : state.depth + 1 };
    // Keep the app's Log / Board URL, and give mobile Back its expected meaning.
    history[wide.matches ? "replaceState" : "pushState"]({ ...history.state, pushupSettings: next }, "");
    render(id, true);
  }

  for (const { link, section } of destinations) {
    link.addEventListener("click", event => {
      event.preventDefault();
      navigate(section.id);
    });
  }
  back.addEventListener("click", () => {
    const state = entry();
    if (state?.depth > 1) history.back();
    else {
      history.replaceState({ ...history.state, pushupSettings: { ...state, section: null } }, "");
      render(null, true);
    }
  });
  window.addEventListener("popstate", () => {
    if (dialog.open && !closing) {
      const state = entry();
      if (state) render(state.section, true);
      else dialog.close();
    }
    closing = false;
  });
  dialog.addEventListener("close", () => {
    const state = entry();
    if (state) {
      closing = true;
      history.go(-state.depth);
    }
  });
  wide.addEventListener("change", () => {
    const state = entry();
    // A desktop pane has one history entry. Give its compact equivalent the
    // same list -> detail history as a section opened on a phone.
    if (dialog.open && !wide.matches && current && state?.depth === 1) {
      history.replaceState({ ...history.state, pushupSettings: { ...state, section: null } }, "");
      history.pushState({ ...history.state, pushupSettings: { ...state, section: current.section.id, depth: 2 } }, "");
    }
    const focusHidden = dialog.querySelector(".settings-nav").contains(document.activeElement)
      || document.activeElement === back || document.activeElement === title
      || current?.section.querySelector("h3") === document.activeElement;
    render(current?.section.id, focusHidden, !focusHidden);
    if (dialog.open && content.contains(document.activeElement)) {
      document.activeElement.scrollIntoView({ block: "nearest", behavior: "instant" });
    }
  });
  const visibility = new MutationObserver(() => {
    for (const { link, section } of destinations) link.hidden = section.hidden;
    if (current?.section.hidden) render(null, true);
  });
  for (const { section } of destinations) visibility.observe(section, { attributes: true, attributeFilter: ["hidden"] });

  window.PushupSettings = {
    open(id) {
      if (closing) return;
      session = `${Date.now()}-${Math.random()}`;
      history.pushState({ ...history.state, pushupSettings: { session, section: null, depth: 1 } }, "");
      render(null);
      dialog.showModal();
      if (id) navigate(id);
    },
  };
  render(null);
})();
