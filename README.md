# Palma Framework

Framework Go opinionato in costruzione, ispirato a Symfony e Spring Boot.
Focalizzato su backend e API, con dependency injection generata a build time.
HTTP è il primo trasporto previsto; il nucleo resta indipendente dal protocollo
per consentire future integrazioni, per esempio gRPC.

Stato attuale: generatore DI con cleanup delle risorse, CLI `pfw` e API HTTP
di esempio, lifecycle applicativo e adapter per server `net/http`.
Gli adapter dedicati ad altre librerie HTTP non sono ancora inclusi.
Il nome usato nel codice è `pfw`.

## Principi

- Costruttori Go normali, dipendenze esplicite.
- Wiring generato come codice Go leggibile.
- Errori del grafo durante la generazione.
- Nessuna reflection o service locator per la DI a runtime.
- Nessuna struttura di cartelle o nomenclatura obbligatoria: MVC, architettura
  esagonale e organizzazione per funzionalità sono scelte dell'applicazione.
- Libreria HTTP a scelta dell'applicazione: standard library, Echo, Chi, Gin
  o altro, con integrazioni opzionali previste dall'architettura.

## Sviluppo

Richiede Go 1.26 o successivo.

```sh
go run ./cmd/pfw generate ./examples/httpapi/internal/bootstrap
go run ./cmd/pfw generate -check ./examples/httpapi/internal/bootstrap
go test ./...
go run ./examples/httpapi/cmd/api
```

L'esempio espone `GET http://localhost:8080/users` e usa il wiring generato
da registrazioni manuali e discovery annotata. Routing e risposte JSON usano Echo v5; il lifecycle
gestisce il server HTTP tramite `transport/httpserver`.

L'[esempio API completo](examples/httpapi/README.md) organizza dominio, servizi
e adapter per funzionalità e implementa creazione, ricerca ed elenco utenti.

La [proposta di architettura](docs/architecture.md) separa il nucleo implementato
dalle convenzioni e funzionalità ancora da decidere.
La [guida dell'API DI](docs/di-api-proposal.md) descrive registrazione, generazione
e limiti della prima versione.

Il module path è `github.com/palma99/palma-framework`; il nome del package è `pfw`.

L'API usa `pfw.Constructors` per registrare i costruttori e
`pfw.Implementation` (o il suo alias `pfw.Bind`) per collegare le interfacce.
Con `pfw.Discover` può anche registrare automaticamente le funzioni annotate
`//pfw:coconut` nei package selezionati, combinandole con le registrazioni manuali.
Il binding è esplicito di default. `pfw.AutoBind()` dentro un modulo abilita
la selezione di un'implementazione concreta unica fra i suoi provider; con più
candidati occorre un binding esplicito.

I costruttori possono restituire `T`, `(T, error)` oppure
`(T, func() error, error)` per le risorse. In quest'ultimo caso il composition
root usa `pfw.BuildWithCleanup`, che restituisce il cleanup da eseguire alla
chiusura. L'esempio `examples/cleanup` mostra questo contratto.

Il [lifecycle applicativo](docs/lifecycle.md) coordina avvio, supervisione,
arresto con timeout e cleanup finale. L'esempio HTTP usa `RunSignals()` per
gestire SIGINT e SIGTERM.

Il [package HTTP](docs/http.md) offre risposte tipizzate, decoder JSON e mapping
degli errori applicativi indipendenti dal router. L'esempio Echo lo usa attraverso
un mapper iniettato nel controller.
