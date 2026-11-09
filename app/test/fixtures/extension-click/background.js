// The toolbar button has no popup: a click is reported to the page in front.
chrome.action.onClicked.addListener((tab) => {
  chrome.tabs.sendMessage(tab.id, { clicked: true });
});
