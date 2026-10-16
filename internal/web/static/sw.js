// Service worker do Claude Fleet. Existe para o navegador tratar o painel
// como app instalável e para servir de base às notificações push.
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
