# API di esempio

Backend Echo con una struttura per funzionalità e separazione fra dominio,
servizi applicativi e adapter. È una scelta di questo esempio: Palma non
richiede cartelle, nomi o architetture specifiche.

```text
httpapi/
├── cmd/api/main.go                 # entry point e lifecycle
└── internal/
    ├── bootstrap/                  # composizione dei moduli e wiring generato
    │   ├── compose.go
    │   ├── generate.go
    │   └── pfw_gen.go
    ├── config/                     # configurazione fornita dall'entry point
    ├── platform/http/              # router principale e server HTTP
    └── user/                       # funzionalità utenti
        ├── domain/                 # tipi, invarianti ed errori
        ├── application/            # operazioni e port Repository
        └── adapter/
            ├── http/               # controller Echo, DTO e mapping degli errori
            ├── memory/             # repository concorrente in memoria
            └── postgres/           # repository persistente e pool con cleanup
```

Il dominio non importa Palma, Echo o lo storage. L'application dipende dal
dominio e dichiara l'interfaccia dello storage. Gli adapter implementano i
confini applicativi. `bootstrap` collega tutto con registrazioni esplicite e
wiring automatico; i moduli dichiarativi restano nello stesso package.
Il modulo `Users` usa `pfw.Discover("../user/...")`: i costruttori di storage,
servizio e controller sono marcati `//pfw:coconut`. Router e server restano
registrati manualmente. `Users` abilita `pfw.AutoBind()` per il mapper HTTP;
il repository viene selezionato con un binding esplicito, diverso per local
rispetto a staging/uat/production. Il codice applicativo dipende dall'interfaccia.

Una nuova funzionalità può affiancare `internal/user`, con i propri servizi
e adapter. `platform/http` raccoglie le route; `bootstrap` raccoglie i provider.
Non serve creare nuovi livelli o interfacce per ogni struct.

## Esecuzione

Dalla root del repository:

```sh
go run ./cmd/pfw generate -env local ./examples/httpapi/internal/bootstrap
go run ./cmd/pfw generate -env local -check ./examples/httpapi/internal/bootstrap
go run ./cmd/pfw inspect ./examples/httpapi/internal/bootstrap
go test -race ./examples/httpapi/...
go run ./cmd/pfw run -env local ./examples/httpapi/cmd/api
```

In alternativa alla prima riga: `go generate ./examples/httpapi/internal/bootstrap`.
In locale l'API ascolta su `:8080`, con un utente iniziale `Ada` (ID `1`). Indirizzo e
nome sono caricati da environment o `.env` in `config.Config`, con default e validazione
prima di costruire il server. Le sottostruct sono `HTTP` e `Seed`. Il prefisso è
`HTTPAPI_`: `HTTP_ADDRESS`, `SEED_USER_NAME`, `HTTP_READ_HEADER_TIMEOUT` e `HTTP_SHUTDOWN_TIMEOUT`. I timeout hanno
default `5s` e devono essere positivi.

```sh
cp examples/httpapi/.env.example .env
HTTPAPI_HTTP_ADDRESS=:9090 HTTPAPI_HTTP_SHUTDOWN_TIMEOUT=10s go run ./cmd/pfw run -env local ./examples/httpapi/cmd/api
```

La [guida configurazione](../../docs/configuration.md) descrive parsing, default
e validazione. Il file `.env` viene letto dalla directory corrente se presente,
con precedenza all'environment del processo. La configurazione viene caricata
dall'entry point prima del wiring.

## Endpoint

| Richiesta | Esito |
| --- | --- |
| `GET /users` | 200, elenco utenti in ordine di creazione. |
| `POST /users` con JSON `{"name":"Grace"}` | 201, utente creato e header `Location`. |
| `GET /users/:id` | 200, utente trovato; 404 se non esiste. |

Il nome viene ripulito dagli spazi iniziali/finali e deve contenere da 1 a 100
caratteri Unicode. La validazione appartiene al dominio e vale anche senza HTTP.
Il repository assegna ID incrementali e protegge letture/scritture concorrenti.
I dati vengono persi alla chiusura del processo; nomi duplicati sono consentiti.

```sh
curl http://localhost:8080/users
curl -i -X POST http://localhost:8080/users -H 'Content-Type: application/json' -d '{"name":"Grace"}'
curl http://localhost:8080/users/2
```

Gli errori applicativi degli endpoint hanno forma:

```json
{
  "code": "validation_failed",
  "message": "invalid input",
  "fields": {"name": "name is required"}
}
```

JSON malformato o tipi errati producono 400; Content-Type diverso da JSON
produce 415; input semanticamente invalido produce 422; utente mancante 404.
Campi JSON sconosciuti o più valori JSON nello stesso body producono 400;
body oltre 1 MiB produce 413. Il decoder e le risposte sono forniti dal package
HTTP di Palma. `NewErrorMapper` viene scoperto e iniettato nel controller:
la traduzione degli errori di dominio rimane configurata nell'adapter HTTP.

## Ambienti e Postgres

Una sola discovery include i provider della funzionalità. Il binding comune
di `application.Repository` seleziona Postgres; `ForEnv("local", Bind[...])`
seleziona memory. `generate -env local` genera soltanto il grafo locale;
per staging/uat/production occorre rigenerare il wiring con il relativo `-env`.
La connessione DB non viene aperta in locale.

In staging/uat/production impostare `HTTPAPI_DB_DSN` (DSN pgx/PostgreSQL) e, se
necessario, `HTTPAPI_DB_CONNECT_TIMEOUT` (default `5s`). La configurazione DB
è obbligatoria soltanto fuori da local. Creare la tabella applicando `schema.sql`
al database scelto prima dell'avvio: il framework non applica migrazioni implicite.
L'adapter usa [pgx tramite database/sql](https://github.com/jackc/pgx/wiki/Getting-started-with-pgx-through-database-sql).
Le query usano parametri; il cleanup del pool viene eseguito dopo lo shutdown HTTP.
Postgres conserva i dati e non aggiunge automaticamente l'utente di seed locale.

```sh
go run ./cmd/pfw run -env staging ./examples/httpapi/cmd/api
go run ./cmd/pfw inspect -env staging ./examples/httpapi/internal/bootstrap
```

`run` legge i file dalla root del modulo; `-env-dir` può selezionare un'altra
directory. In particolare `.env.staging` e `.env.production` possono avere DSN
diversi con la stessa implementazione. I test unitari del repository SQL usano
un driver locale: non richiedono né modificano un database Postgres esterno.
Gli errori interni producono 500 senza esporre la causa al client.
Gli errori di routing fuori da questi endpoint restano gestiti da Echo.

I test passano attraverso il composition root generato e verificano il flusso
creazione/ricerca/elenco, validazione, errori e concorrenza del repository.
SIGINT/SIGTERM attivano il lifecycle e lo shutdown con timeout di 5 secondi.
