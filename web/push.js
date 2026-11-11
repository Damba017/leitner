// Web Push on this device: whether it can work here, and subscribing (from
// nestje's push.js). The Meldingen tab (notify.js) uses it.

const ua = navigator.userAgent;
const ios = /iPhone|iPad|iPod/.test(ua) || (/Macintosh/.test(ua) && navigator.maxTouchPoints > 1);
// The service worker gets this long to start before push counts as broken.
const READY_TIMEOUT = 10000; // ms

// unsupported says why notifications can't work in this browser; "" if they can.
export function unsupported() {
  if (!window.isSecureContext) {
    return "Meldingen werken alleen via https (of op localhost).";
  }
  if ("serviceWorker" in navigator && "PushManager" in window && "Notification" in window) {
    return "";
  }
  if (ios) {
    return "Op iPhone en iPad werken meldingen alleen vanuit Leitner op je beginscherm: tik in Safari op Deel → Zet op beginscherm, open Leitner daar en zet meldingen hier aan.";
  }
  return "Deze browser ondersteunt geen meldingen.";
}

// blocked: the user said no to notifications for this site.
export const blocked = () => "Notification" in window && Notification.permission === "denied";

// deviceLabel names this device in the list, e.g. "Safari · Mac".
export function deviceLabel() {
  let device = "";
  if (/iPhone/.test(ua)) {
    device = "iPhone";
  } else if (ios) {
    device = "iPad";
  } else if (/Android/.test(ua)) {
    device = "Android";
  } else if (/Windows/.test(ua)) {
    device = "Windows";
  } else if (/Macintosh/.test(ua)) {
    device = "Mac";
  } else if (/Linux/.test(ua)) {
    device = "Linux";
  }
  let browser = "Browser";
  if (navigator.standalone || matchMedia("(display-mode: standalone)").matches) {
    browser = "App";
  } else if (/Edg\//.test(ua)) {
    browser = "Edge";
  } else if (/Firefox\/|FxiOS/.test(ua)) {
    browser = "Firefox";
  } else if (/Chrome\/|CriOS/.test(ua)) {
    browser = "Chrome";
  } else if (/Safari\//.test(ua)) {
    browser = "Safari";
  }
  return device ? `${browser} · ${device}` : browser;
}

export function fromBase64url(s) {
  const bin = atob(s.replace(/-/g, "+").replace(/_/g, "/"));
  return Uint8Array.from(bin, (c) => c.charCodeAt(0));
}

function sameKey(buf, want) {
  const a = new Uint8Array(buf);
  return a.length === want.length && a.every((b, i) => b === want[i]);
}

// registration waits for the service worker; null when it doesn't start.
export async function registration() {
  try {
    await navigator.serviceWorker.register("/sw.js", { scope: "/" });
    const timeout = new Promise((resolve) => setTimeout(() => resolve(null), READY_TIMEOUT));
    return await Promise.race([navigator.serviceWorker.ready, timeout]);
  } catch {
    return null;
  }
}

// currentSubscription is this browser's subscription, or null. One made with
// another server key (e.g. a fresh database) can't receive anything, so it is
// dropped and "Meldingen aan" makes a new one.
export async function currentSubscription(reg, key) {
  let sub = await reg.pushManager.getSubscription();
  const old = sub?.options?.applicationServerKey;
  if (sub && old && !sameKey(old, key)) {
    await sub.unsubscribe();
    sub = null;
  }
  return sub;
}

// subscribe asks for permission and subscribes this browser. It must be the
// first thing a click does: Safari only asks for permission then.
export async function subscribe(reg, key) {
  try {
    return await reg.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: key });
  } catch {
    throw new Error(blocked() ? "Je hebt meldingen niet toegestaan." : "Meldingen aanzetten lukte niet.");
  }
}

// subscriptionBody is what POST /api/push/subscriptions wants for sub.
export function subscriptionBody(sub) {
  const j = sub.toJSON();
  return { endpoint: j.endpoint, keys: { p256dh: j.keys.p256dh, auth: j.keys.auth }, label: deviceLabel() };
}
