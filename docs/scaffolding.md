# Creare un progetto da template

La CLI include i template nel binario. Per elencarli:

```sh
pfw templates
```

- `hello-world`: applicazione minimale che stampa `Hello, world!`, con composition root.
- `api`: API esagonale con router a scelta, storage in memoria in `local` e
  repository PostgreSQL in `staging` e `prod`.

## Dal checkout del framework

```sh
go run ./cmd/pfw new -template hello-world \
  -module example.com/hello -env dev -framework-dir . /tmp/palma-hello

go run ./cmd/pfw new -template api \
  -module example.com/myapi -env local -framework-dir . /tmp/palma-api
```

La directory di destinazione deve essere nuova e il suo parent deve esistere.
La CLI rifiuta directory e file già presenti, senza sovrascriverli. I file vengono
prima renderizzati e il codice Go formattato; un errore di scrittura rimuove il
progetto parziale creato dal comando. Non vengono installate dipendenze né avviati
processi durante la creazione.

`-module` è il module path della tua applicazione. `-env` sceglie il primo ambiente
usato per avvio e generazione. Per `api` il default è `local` e sono dichiarati
`local`, `staging` e `prod`; `-env` deve scegliere uno di questi tre nomi. Per
`hello-world` il default resta `dev` e il nome è libero.

`-framework-dir` scrive nel nuovo `go.mod` un `replace` assoluto al checkout locale.
Il progetto segue così le modifiche locali del framework. Quel percorso deve
rimanere disponibile finché usi il `replace`.

## Con una versione pubblicata

Una CLI installata da una versione pubblicata usa automaticamente la propria
versione come dipendenza del progetto. Puoi scegliere una versione esplicita:

```sh
pfw new -template api -module example.com/myapi \
  -framework-version v0.1.0 ./myapi
```

`v0.1.0` è un esempio: specificare un tag o una pseudo-versione effettivamente
pubblicati. La CLI di sviluppo richiede `-framework-dir` oppure
`-framework-version`; non inserisce una versione di release inventata.
I due flag sono alternativi.

## Scelta del router HTTP

Il template `api` accetta `-router stdlib` (default) oppure `-router echo`:

```sh
go run ./cmd/pfw new -template api -router echo \
  -module example.com/echoapi -env local -framework-dir . /tmp/palma-echo-api
```

`stdlib` usa `net/http`; `echo` usa Echo v5 e ne aggiunge la dipendenza esplicita
al `go.mod`. Il flag è disponibile soltanto per `api`; nomi non supportati sono
rifiutati prima di creare file. `pfw templates` mostra anche i router disponibili.

I file comuni risiedono in `internal/scaffold/templates/api`. I file specifici
del router sono inclusi da `internal/scaffold/routers/<nome>` e applicati come
variante del trasporto HTTP. Per aggiungere un router in futuro, aggiungere
la sua voce al registro `Routers()` (con le eventuali dipendenze) e i template
di controller e server nella nuova directory. Dominio, application, configurazione,
storage e test del contratto HTTP rimangono condivisi. Gin e Chi non sono
ancora disponibili.

## Avvio e generazione

Entrambi gli skeleton richiedono Go 1.26 o successivo. Il `go.mod` registra la CLI
come tool, così `go tool pfw` usa la stessa versione della libreria.

Per l'API creata sopra:

```sh
cd /tmp/palma-api
go mod tidy
cp env/.env.example env/.env
go tool pfw run -env local
```

Per hello world:

```sh
cd /tmp/palma-hello
go mod tidy
go tool pfw run -env dev
```

`run` genera il wiring per l'ambiente selezionato, compila e avvia. Per separare
le operazioni, il progetto include anche una direttiva `go:generate`:

```sh
go generate ./internal/bootstrap
go tool pfw generate -env local -check
go test ./...
go build -o bin/api ./cmd/api
PFW_ENV=local ./bin/api
```

Cambiare ambiente richiede rigenerare e ricompilare. La direttiva `go:generate`
usa l'ambiente iniziale scelto alla creazione. Ogni skeleton include un README
con i comandi specifici del progetto.

## Base esagonale dell'API

```text
cmd/api/                     avvio e lifecycle
internal/
  bootstrap/                 composizione DI e wiring generato
  config/                    configurazione tipizzata e validazione
  platform/http/             router e server
  platform/database/         provider MainDB, primary e replica opzionale
  item/
    domain/                  entità e invarianti
    application/             casi d'uso e porta Repository
    infrastructure/
      http/                  handler, DTO e mapping degli errori
      memory/                implementazione locale in memoria
      postgres/              repository SQL per staging/prod
```

Dominio e application non importano Palma né il trasporto HTTP. Le implementazioni dell'infrastruttura
dipendono dalle porte dell'application; il composition root sceglie e collega
le implementazioni. Lo storage memory è pronto all'uso e perde i dati al riavvio.
Il composition root seleziona `memory.Store` in `local` e `postgres.Store` in
`staging` e `prod`, tramite binding espliciti e `ForEnv("local", ...)`. Il
repository SQL riceve `database.MainDB` nel costruttore; il provider annotato
`internal/platform/database.OpenMainDB` è incluso nella discovery. In locale
il provider non è raggiungibile e non viene aperto alcun pool SQL.

I file dotenv risiedono nella cartella `env/`, configurata con `env_dir = "./env"`
in `pfw.toml`. Anche il loader applicativo usa `env/` come default se il binario
viene avviato dalla root del modulo. I progetti includono `env/.env.local`, `env/.env.staging` e `env/.env.prod` con default
versionabili e DSN vuoti. Dopo i valori comuni di `env/.env`, il loader legge il
file dell'ambiente e infine `env/.env.<ambiente>.local`; le variabili del processo
prevalgono su tutti i file. Le impostazioni di staging/prod azzerano il DSN
comune: fornire `APP_DB_DSN` separatamente con variabili del processo o nei file
ignorati `env/.env.staging.local` e `env/.env.prod.local`. Il DSN è obbligatorio fuori da
`local`; la configurazione fallisce prima dell'avvio in sua assenza.

PostgreSQL pgx è incluso e registrato dal provider. Prima dell'avvio SQL,
eseguire `go tool pfw migrate up -env staging` oppure `-env prod`. Lo scaffold
include `migrations/000001_create_items.sql` con sezioni `-- +pfw Up` e
`-- +pfw Down`; le
migration non vengono eseguite automaticamente all’avvio. Vedi la
[guida migration](migrations.md). Gli ambienti staging e prod hanno limiti iniziali del
pool rispettivamente di 10/2 e 40/10 connessioni aperte/inattive, personalizzabili
con `APP_DB_*`. `env/.env.example` documenta tutte le opzioni.

`APP_DB_REPLICA_DSN` configura una replica opzionale `report`; il repository
può selezionare `db.Primary()` oppure `db.Replica("report")` a runtime. I metodi
inclusi nello starter usano il primary. Il cleanup è gestito da `BuildWithCleanup`
e dal lifecycle; il context di avvio permette di cancellare connessione e ping.
Vedi la [guida database](database.md).

```sh
go tool pfw run -env local
go tool pfw run -env staging
go tool pfw run -env prod
```

Cambiare ambiente richiede rigenerare il wiring. I test generati usano `PFW_ENV`
(default `local`); per verificare il grafo staging, per esempio:

```sh
go tool pfw generate -env staging
PFW_ENV=staging go test ./...
```

I test SQL usano un driver locale e non richiedono database esterni.

L'API offre `GET /health`, `GET /items`, `GET /items/{id}` e `POST /items`.
Include decoding JSON con limiti e rifiuto dei campi sconosciuti, validazione
del nome, risposte JSON, errori pubblici senza dettagli interni e test del flusso
HTTP attraverso il wiring generato. Configurazione e file dotenv usano il
prefisso `APP_`; il lifecycle gestisce SIGINT/SIGTERM e shutdown con timeout.

Entrambi i template includono `pfw.Discover("../...")` nella composizione:
la discovery copre tutti i package sotto `internal`. I costruttori dei provider
sono annotati con `//pfw:coconut`; per aggiungere un nuovo provider basta
annotare il suo costruttore, senza mantenere una lista `pfw.Constructors`.
La composizione radice include `pfw.AutoBind()`: un unico provider compatibile
viene selezionato automaticamente per ciascuna interfaccia. I moduli ereditano
questa impostazione, salvo un proprio `AutoBind()` oppure `AutoBind(false)`. Con più
candidati occorre un `pfw.Bind` esplicito, che prevale sull'autobinding.
Le factory di dominio e gli helper che non sono provider DI non sono annotati.

L'API usa il componente lifecycle HTTP fornito dal framework per entrambi i router. Dopo il bind della
porta scrive `server started address=<indirizzo> environment=<nome>`, mostrando
l'indirizzo effettivamente assegnato anche con porta `0`. Se il bind fallisce,
l'avvio restituisce l'errore senza stampare un log di readiness. I test generati
verificano sia la raggiungibilità del server dopo il log sia l'assenza del log
quando la porta è occupata.

Handler e componente lifecycle ricevono `logging.Logger` tramite DI. Il generatore
fornisce il logger standard e il componente HTTP come fallback: non vengono
creati file applicativi per questi provider. Per personalizzare il comportamento,
registrare provider applicativi e gli eventuali binding, anche attraverso `ForEnv`.
La [guida logging](logging.md) descrive le precedenze e le opzioni.

Il mapper degli errori è un provider annotato (`NewErrorMapper`) in
`internal/item/infrastructure/http/error_mapper.go`. Gli handler ricevono
`pfwhttp.ErrorMapper` dal grafo; l'autobinding seleziona `*pfwhttp.Mapper`.
Le regole applicative vengono definite nel provider; per una propria
implementazione registrare il relativo costruttore e aggiungere un binding
esplicito se rimangono disponibili più mapper compatibili.

## Percorsi dei comandi in `pfw.toml`

`pfw new` crea `pfw.toml` accanto a `go.mod`. Per il template API:

```toml
bootstrap = "./internal/bootstrap"
main = "./cmd/api"
env_dir = "./env"
migrations_dir = "./migrations"
```

Il template hello-world usa `main = "./cmd/app"`. Puoi modificare questi
percorsi quando sposti i package. I percorsi `main` e `bootstrap` sono directory di package Go relative alla
root del modulo, non nomi di singoli file `.go`. `env_dir` è la directory dei
file dotenv, anch’essa relativa al modulo. Per `run`, la precedenza è
`-env-dir`, `PFW_ENV_DIR`, `env_dir` in TOML, poi la root del modulo.

```sh
go tool pfw generate -env local
go tool pfw inspect -env local
go tool pfw run -env local
go tool pfw run -env local -- -verbose
```

Se ometti i package, la CLI cerca il `go.mod` più vicino risalendo dalla directory
corrente e legge il `pfw.toml` accanto a esso. Funziona anche dalle sottocartelle.
Senza file TOML, o per chiavi omesse, i default sono `./internal/bootstrap` e
`./cmd/app`. Per un'API esistente aggiungi `main = "./cmd/api"`.
I package passati esplicitamente prevalgono e mantengono la risoluzione dalla
directory corrente (`.` permette di selezionare il package corrente).
Il TOML viene letto solo quando serve un target implicito: sintassi errata,
chiavi sconosciute, tipi errati e percorsi vuoti producono un errore esplicito.
L'ambiente continua a essere selezionato con i flag esistenti e, per `run`,
con `PFW_ENV`. `run` trova la composizione raggiungibile dal main selezionato.

Per controller, montaggio e middleware nativi vedere la [guida al routing](routing.md).
