"use strict";

const prices = { Standard: 299, Student: 99, Team: 249 };

// Navigation menu for narrow screens.
const nav = document.querySelector(".nav");
const menuButton = nav.querySelector(".menu-button");

function setMenu(open) {
  nav.classList.toggle("open", open);
  menuButton.setAttribute("aria-expanded", String(open));
}

menuButton.addEventListener("click", () => setMenu(!nav.classList.contains("open")));
nav.querySelectorAll(".nav-links a").forEach((link) => link.addEventListener("click", () => setMenu(false)));
document.addEventListener("keydown", (event) => {
  if (event.key === "Escape" && nav.classList.contains("open")) {
    setMenu(false);
    menuButton.focus();
  }
});

// Schedule tabs, following the WAI-ARIA tabs pattern.
const tabs = [...document.querySelectorAll('[role="tab"]')];

function selectTab(tab) {
  for (const other of tabs) {
    const selected = other === tab;
    other.setAttribute("aria-selected", String(selected));
    other.tabIndex = selected ? 0 : -1;
    document.getElementById(other.getAttribute("aria-controls")).hidden = !selected;
  }
  tab.focus();
}

tabs.forEach((tab, index) => {
  tab.addEventListener("click", () => selectTab(tab));
  tab.addEventListener("keydown", (event) => {
    const moves = {
      ArrowRight: (index + 1) % tabs.length,
      ArrowLeft: (index - 1 + tabs.length) % tabs.length,
      Home: 0,
      End: tabs.length - 1,
    };
    if (event.key in moves) {
      event.preventDefault();
      selectTab(tabs[moves[event.key]]);
    }
  });
});

// Registration form.
const form = document.getElementById("register-form");
const nameInput = document.getElementById("name");
const emailInput = document.getElementById("email");
const ticketSelect = document.getElementById("ticket");
const teamField = document.getElementById("team-field");
const teamInput = document.getElementById("team-size");
const total = document.getElementById("total");
const status = document.getElementById("form-status");

const euro = new Intl.NumberFormat("en-IE", { style: "currency", currency: "EUR", maximumFractionDigits: 0 });

function teamSize() {
  const n = Number(teamInput.value);
  return Number.isInteger(n) ? n : NaN;
}

function update() {
  const ticket = ticketSelect.value;
  teamField.hidden = ticket !== "Team";
  const people = ticket === "Team" ? teamSize() : 1;
  total.textContent = Number.isFinite(people) && people > 0 ? `Total: ${euro.format(prices[ticket] * people)}` : "Total: enter a team size";
}

ticketSelect.addEventListener("change", update);
teamInput.addEventListener("input", update);

document.querySelectorAll(".choose").forEach((button) => {
  button.addEventListener("click", () => {
    ticketSelect.value = button.dataset.ticket;
    update();
    document.getElementById("register").scrollIntoView();
    nameInput.focus();
  });
});

function setError(input, message) {
  const error = document.getElementById(`${input.id}-error`);
  if (message) {
    input.setAttribute("aria-invalid", "true");
    input.setAttribute("aria-describedby", error.id);
    error.textContent = message;
    error.hidden = false;
  } else {
    input.removeAttribute("aria-invalid");
    input.removeAttribute("aria-describedby");
    error.textContent = "";
    error.hidden = true;
  }
  return !message;
}

const emailPattern = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

form.addEventListener("submit", (event) => {
  event.preventDefault();
  status.textContent = "";
  const checks = [
    [nameInput, nameInput.value.trim() ? "" : "Enter your full name."],
    [emailInput, emailPattern.test(emailInput.value.trim()) ? "" : "Enter a valid email address, like name@example.com."],
  ];
  if (ticketSelect.value === "Team") {
    const n = teamSize();
    checks.push([teamInput, n >= 3 && n <= 20 ? "" : "Team tickets are for 3 to 20 people."]);
  } else {
    setError(teamInput, "");
  }
  const invalid = checks.filter(([input, message]) => !setError(input, message)).map(([input]) => input);
  if (invalid.length > 0) {
    invalid[0].focus();
    return;
  }
  status.textContent = `You're registered, ${nameInput.value.trim()}! We sent the details to ${emailInput.value.trim()}.`;
});

update();
