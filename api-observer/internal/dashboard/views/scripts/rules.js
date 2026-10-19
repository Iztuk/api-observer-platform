const workspace = document.getElementById("rules-workspace");
const details = document.getElementById("rules-details");
const closeButton = document.getElementById("rules-details-close");

let selectedIndex = null;

function closeRuleDetails() {
  if (!workspace || !details) return;

  // Hide the details column.
  workspace.classList.remove("is-open");
  details.hidden = true;

  // Clear the selected rule.
  workspace.querySelectorAll("[data-rule-index]").forEach((button) => {
    button.classList.remove("is-selected");
    button.setAttribute("aria-expanded", "false");
  });

  // Hide all rule details.
  details.querySelectorAll("[data-detail-index]").forEach((panel) => {
    panel.hidden = true;
  });

  selectedIndex = null;
}

function openRuleDetails(index) {
  if (!workspace || !details) return;

  // Clicking the selected rule again closes the panel.
  if (selectedIndex === index) {
    closeRuleDetails();
    return;
  }

  const button = workspace.querySelector(`[data-rule-index="${index}"]`);

  const panel = details.querySelector(`[data-detail-index="${index}"]`);

  if (!button || !panel) return;

  // Reset the previous selection.
  closeRuleDetails();

  // Open the details column.
  workspace.classList.add("is-open");
  details.hidden = false;
  panel.hidden = false;

  // Highlight the selected rule.
  button.classList.add("is-selected");
  button.setAttribute("aria-expanded", "true");

  selectedIndex = index;

  // Start each rule's details at the top.
  details.scrollTop = 0;
}

// Handle clicks on any rule in the list.
workspace?.addEventListener("click", (event) => {
  const button = event.target.closest("[data-rule-index]");

  if (!button || !workspace.contains(button)) return;

  openRuleDetails(button.dataset.ruleIndex);
});

// Close the panel using the X button.
closeButton?.addEventListener("click", closeRuleDetails);
