# Palma Framework

Framework Go opinionato in costruzione, ispirato a Symfony e Spring Boot.
Focalizzato su backend e API, con dependency injection generata a build time.
HTTP è il primo trasporto previsto; il nucleo resta indipendente dal protocollo
per consentire future integrazioni, per esempio gRPC.

Prima release in preparazione: **v0.1.0**, con API in evoluzione e licenza MIT.
Include generatore DI, cleanup delle risorse, CLI `pfw`, configurazione,
lifecycle e skeleton API con router stdlib o Echo v5.
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

## Installazione e primo progetto

Richiede Go 1.26 o successivo. Dopo la pubblicazione del tag `v0.1.0`:

```sh
go install github.com/palma99/palma-framework/cmd/pfw@v0.1.0
pfw version
pfw new -template api -router echo -module example.com/myapi ./myapi
cd myapi
go mod tidy
go tool pfw run -env dev
```

La CLI installata deve essere nel `PATH`. Per un'applicazione minimale scegliere
`-template hello-world`; per l'API standard scegliere `-router stdlib`.
Il progetto include la CLI come tool Go e fissa la stessa versione della libreria.
`pfw.toml` salva i percorsi dei package bootstrap e main: `generate`, `inspect`
e `run` li usano quando ometti il package. Gli argomenti espliciti prevalgono;
vedi [configurazione dei percorsi](docs/scaffolding.md#percorsi-dei-comandi-in-pfwtoml).

Per usare soltanto la libreria in un modulo esistente:

```sh
go get github.com/palma99/palma-framework@v0.1.0
```

Fino alla pubblicazione, usare il checkout locale come descritto sotto.
La [guida al rilascio](docs/releasing.md) include verifiche, packaging e pubblicazione.
La [licenza MIT](LICENSE) copre il codice del framework.

## Sviluppo

Per creare un progetto esterno da uno skeleton incluso nella CLI:

```sh
go run ./cmd/pfw templates
go run ./cmd/pfw new -template api -module example.com/myapi \
  -env dev -framework-dir . /tmp/palma-api
```

Sono disponibili `hello-world` e `api` (architettura esagonale).
Per `api`, `-router stdlib` è il default; `-router echo` genera la variante Echo v5.
La [guida ai template](docs/scaffolding.md) descrive creazione, dipendenze e avvio.

Richiede Go 1.26 o successivo.

```sh
go run ./cmd/pfw generate -env local ./examples/httpapi/internal/bootstrap
go run ./cmd/pfw generate -env local -check ./examples/httpapi/internal/bootstrap
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
Il binding è esplicito di default. `pfw.AutoBind()` abilita la selezione di
un'implementazione concreta unica nella radice o in un modulo. I moduli ereditano
il globale, salvo un proprio `AutoBind(true/false)`; con più candidati occorre
un binding esplicito.

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

La [configurazione](docs/configuration.md) carica environment e file `.env` in struct Go con
default, campi richiesti, parsing tipizzato e validazione. L'esempio HTTP usa
il prefisso `HTTPAPI_` per indirizzo, timeout e utente iniziale.
Le impostazioni possono essere raggruppate in sottostruct tramite `envPrefix`.

Il [logger](docs/logging.md) espone un'interfaccia iniettabile e un'implementazione
standard basata su `log/slog`. Gli skeleton API usano un binding esplicito per
permettere all'applicazione di sostituirla con il proprio logger.

`pfw inspect` mostra provider, provenienza, scope, binding, ordine di costruzione
e cleanup senza scrivere file. Supporta output testuale e `-json`:

```sh
go run ./cmd/pfw inspect ./examples/httpapi/internal/bootstrap
```

La [guida inspect](docs/inspect.md) descrive il report e l'uso in tooling.

La [gestione degli ambienti](docs/environments.md) offre `ForEnv`, override,
binding specifici e wiring generato per un ambiente alla volta:

```sh
go run ./cmd/pfw run -env local ./examples/httpapi/cmd/api
go run ./cmd/pfw inspect -env production ./examples/httpapi/internal/bootstrap
```

## Transazioni applicative

`transaction.Runner.Within` coordina operazioni applicative tramite un context
condiviso. L’integrazione `transaction/sql` permette a più repository di usare
la stessa transazione SQL, con commit, rollback e annidamento controllati.
Vedi la [guida alle transazioni](docs/transactions.md).
