"use strict";

// What counts as "the same site" for saved logins: the exact origin, so
// https://example.com never fills a form on http://example.com, on
// https://example.com.evil.test or on another port.

/** The origin of an http(s) address, or "" for anything else. */
function originOf(raw) {
  try {
    const url = new URL(raw);
    if (url.protocol !== "https:" && url.protocol !== "http:") return "";
    if (url.username || url.password || !url.hostname) return "";
    return url.origin;
  } catch {
    return "";
  }
}

/** Exact origin match; both sides must be real http(s) origins. */
function sameOrigin(a, b) {
  const left = originOf(a);
  return left !== "" && left === originOf(b);
}

module.exports = { originOf, sameOrigin };
