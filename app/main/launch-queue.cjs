"use strict";

// A second launch may arrive before the gateway has an origin. Opening a
// window then creates an invalid navigation that the first launch reuses.
class LaunchQueue {
  constructor(project, open, focus) {
    this.pending = new Set([project]);
    this.open = open;
    this.focus = focus;
    this.isReady = false;
    this.focusRequested = false;
  }

  request(project) {
    if (this.isReady) {
      if (project) this.open(project);
      else this.focus();
    } else if (project) this.pending.add(project);
    else this.focusRequested = true;
  }

  pause() { this.isReady = false; }

  ready() {
    this.isReady = true;
    const pending = [...this.pending];
    this.pending.clear();
    for (const project of pending) this.open(project);
    if (this.focusRequested) this.focus();
    this.focusRequested = false;
  }
}

module.exports = { LaunchQueue };
