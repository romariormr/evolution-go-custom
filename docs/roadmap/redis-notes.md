# Nota — Redis (adiado)

> Status: NÃO INICIADO. Decisão: deixar pra quando houver uso real definido.

## Achado (2026-07-14)

`evolution-go` não usa Redis em nenhum lugar do código (zero import, zero referência),
diferente da Evolution API original (`CACHE_REDIS_*`). A stack roda `replicas: 1`
(instância única, sem load balancing) — hoje não há estado pra compartilhar entre réplicas.

Subir uma stack Redis agora seria decorativa: nada consumiria.

## Quando revisitar

- Se a stack for escalada pra múltiplas réplicas → Redis vira necessário pra
  compartilhar estado (dedup de mensagem, sessões, cache de instância) entre elas.
- Se surgir uma feature específica que se beneficia de cache compartilhado
  (ex.: rate limiting entre instâncias, cache de resposta de API externa).

Nesses casos: definir o que exatamente vai usar Redis primeiro, then plugar o código —
não criar a infra antes do consumidor.
