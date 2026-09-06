const CACHE = "crest-worker-shell-v1";

self.addEventListener("install", (event) => {
  event.waitUntil(
    caches.open(CACHE).then(async (cache) => {
      const response = await fetch("./index.html");
      if (!response.ok) throw new Error("worker shell unavailable");
      const html = await response.text();
      const assets = [...html.matchAll(/(?:src|href)=\"([^\"]+)\"/g)]
        .map((match) => match[1])
        .filter((asset) => asset.startsWith("./") && !asset.startsWith("./api/"));
      await cache.put("./index.html", new Response(html, { headers: { "Content-Type": "text/html" } }));
      await cache.addAll(["./", ...assets]);
    }).then(() => self.skipWaiting()),
  );
});

self.addEventListener("activate", (event) => {
  event.waitUntil(
    caches.keys().then((keys) => Promise.all(
      keys.filter((key) => key.startsWith("crest-worker-shell-") && key !== CACHE).map((key) => caches.delete(key)),
    )).then(() => self.clients.claim()),
  );
});

self.addEventListener("fetch", (event) => {
  const request = event.request;
  if (request.method !== "GET") return;
  const url = new URL(request.url);
  if (url.origin !== self.location.origin || request.headers.has("Authorization") || url.pathname.includes("/api/") || url.pathname.includes("/v1/")) return;
  event.respondWith(
    fetch(request).then((response) => {
      if (response.ok) void caches.open(CACHE).then((cache) => cache.put(request, response.clone()));
      return response;
    }).catch(() => caches.match(request).then((cached) => cached || (request.mode === "navigate" ? caches.match("./index.html") : Response.error()))),
  );
});
