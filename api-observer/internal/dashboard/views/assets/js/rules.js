var __getOwnPropNames = Object.getOwnPropertyNames;
var __commonJS = (cb, mod) => function __require() {
  try {
    return mod || (0, cb[__getOwnPropNames(cb)[0]])((mod = { exports: {} }).exports, mod), mod.exports;
  } catch (e) {
    throw mod = 0, e;
  }
};

// internal/dashboard/views/scripts/rules.js
var require_rules = __commonJS({
  "internal/dashboard/views/scripts/rules.js"() {
    var workspace = document.getElementById("rules-workspace");
    var details = document.getElementById("rules-details");
    var closeButton = document.getElementById("rules-details-close");
    var selectedIndex = null;
    function closeRuleDetails() {
      if (!workspace || !details) return;
      workspace.classList.remove("is-open");
      details.hidden = true;
      workspace.querySelectorAll("[data-rule-index]").forEach((button) => {
        button.classList.remove("is-selected");
        button.setAttribute("aria-expanded", "false");
      });
      details.querySelectorAll("[data-detail-index]").forEach((panel) => {
        panel.hidden = true;
      });
      selectedIndex = null;
    }
    function openRuleDetails(index) {
      if (!workspace || !details) return;
      if (selectedIndex === index) {
        closeRuleDetails();
        return;
      }
      const button = workspace.querySelector(`[data-rule-index="${index}"]`);
      const panel = details.querySelector(`[data-detail-index="${index}"]`);
      if (!button || !panel) return;
      closeRuleDetails();
      workspace.classList.add("is-open");
      details.hidden = false;
      panel.hidden = false;
      button.classList.add("is-selected");
      button.setAttribute("aria-expanded", "true");
      selectedIndex = index;
      details.scrollTop = 0;
    }
    workspace?.addEventListener("click", (event) => {
      const button = event.target.closest("[data-rule-index]");
      if (!button || !workspace.contains(button)) return;
      openRuleDetails(button.dataset.ruleIndex);
    });
    closeButton?.addEventListener("click", closeRuleDetails);
  }
});
export default require_rules();
