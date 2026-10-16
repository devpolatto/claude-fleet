// Service worker do Claude Fleet. Torna o painel instalável e mostra as
// notificações push de sessão aguardando, mesmo com o app fechado.
//
// De propósito, não tem handler de fetch nem usa a Cache API: toda
// requisição (página, API, /stream) vai direto à rede, nada do trabalho fica
// guardado no aparelho e nunca se mostra um dado velho.
'use strict';

self.addEventListener('install', () => {
  self.skipWaiting();
});

self.addEventListener('activate', (event) => {
  event.waitUntil(self.clients.claim());
});

// O servidor manda { title, body, tag, url }, cifrado de ponta a ponta. A
// mesma tag por sessão faz a notificação nova substituir a anterior, e
// renotify faz ela vibrar de novo no lembrete.
self.addEventListener('push', (event) => {
  let m = {};
  try {
    m = event.data ? event.data.json() : {};
  } catch (_) { /* corpo inesperado: mostra o genérico */ }
  event.waitUntil(self.registration.showNotification(m.title || 'Claude Fleet', {
    body: m.body || 'Uma sessão precisa de você.',
    tag: m.tag || 'fleet',
    renotify: true,
    icon: '/icon-192.png',
    data: { url: m.url || '/' },
  }));
});

// Tocar na notificação foca uma janela do painel ou abre uma nova.
self.addEventListener('notificationclick', (event) => {
  event.notification.close();
  const alvo = new URL((event.notification.data && event.notification.data.url) || '/', self.location.origin);
  event.waitUntil((async () => {
    const janelas = await self.clients.matchAll({ type: 'window', includeUncontrolled: true });
    for (const c of janelas) {
      if (new URL(c.url).origin === alvo.origin && 'focus' in c) return c.focus();
    }
    return self.clients.openWindow(alvo.href);
  })());
});
