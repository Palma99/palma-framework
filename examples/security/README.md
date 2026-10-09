# Security: sessioni e documenti

Esempio eseguibile con repository in memoria e binding DI generici. Non richiede
un database né un provider esterno. Il flusso è:

```text
cookie session → SessionCredentials → VerifiedSession → AppPrincipal
              → controller → servizio → policy sul Document → repository
```

`security.go` contiene extractor, authenticator, resolver, policy e servizio;
`store.go` i repository e i dati dimostrativi; `main.go` controller e router;
`compose.go` le registrazioni DI. `pfw_gen.go` viene generato dalla CLI.
La [guida security](../../docs/security.md) documenta i contratti del framework.

## Avvio

Dalla root del repository:

```sh
go generate ./examples/security
go run ./examples/security
```

Il server ascolta su `127.0.0.1:8080`. SIGINT/SIGTERM eseguono lo shutdown
attraverso il lifecycle Palma. Le sessioni dimostrative durano un'ora dall'avvio.

```sh
# GET opzionale: 200, authenticated=false senza cookie
curl http://127.0.0.1:8080/me

# Principal costruito dai dati dell'utente applicativo
curl -b 'session=demo-alice' http://127.0.0.1:8080/me

# Proprietario: 204, aggiorna doc-1
curl -i -X PATCH -b 'session=demo-alice' \
  -H 'Content-Type: application/json' -d '{"title":"Updated"}' \
  http://127.0.0.1:8080/documents/doc-1

# Altro proprietario: 403
curl -i -X PATCH -b 'session=demo-alice' \
  -H 'Content-Type: application/json' -d '{"title":"Denied"}' \
  http://127.0.0.1:8080/documents/doc-2

# Admin nella stessa organizzazione: 204
curl -i -X PATCH -b 'session=demo-admin' \
  -H 'Content-Type: application/json' -d '{"title":"Updated by admin"}' \
  http://127.0.0.1:8080/documents/doc-2
```

Un admin dell'organizzazione `one` non può modificare `doc-3` di `two`.
`demo-bob` possiede `doc-2`; `demo-disabled` produce 403; `demo-expired`,
`demo-revoked`, `demo-deleted` e sessioni sconosciute producono 401.
Anche `/me`, pur opzionale, rifiuta credenziali presenti ma non valide.

Il mapper condiviso gestisce errori di autenticazione, autorizzazione e documenti
mancanti. La policy viene eseguita nel servizio anche fuori da HTTP.
Il router applica `http.CrossOriginProtection` alle richieste che modificano
lo stato, con diniego 403 attraverso lo stesso mapper.

Le sessioni sono fixture pubbliche: l'esempio non include login, generazione di
sessioni, password, emissione dei cookie o una persistenza reale. Un'applicazione
deve implementare questi flussi e la propria protezione delle sessioni/cookie.
Il framework non interpreta ID, ruoli o organizzazioni dell'applicazione.

## Verifica

```sh
go run ./cmd/pfw generate -check ./examples/security
go test -race ./examples/security ./security/... ./transport/echo
```

I test attraversano il composition root generato e verificano sessioni valide,
scadute e revocate, utenti eliminati/disabilitati, ownership, isolamento delle
organizzazioni, autenticazione opzionale, protezione cross-origin e assenza di
modifiche ai documenti quando l'accesso è negato.
