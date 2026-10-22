---
title: dex-client-controller
template: home.html
hide:
  - navigation
  - toc
---

<!--
  The home page is rendered entirely by overrides/home.html (the landing).
  This body only provides the page title and meta description for the <head>.
-->

Self-service SSO for dex: namespaced DexClient and DexConnector custom resources, reconciled into a running dex through
its gRPC API over mutual TLS. No restarts, no secrets in ConfigMaps, and dex's config file is never touched.
