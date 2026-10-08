# Creare un progetto da template

La CLI include i template nel binario. Per elencarli:

```sh
pfw templates
```

- `hello-world`: applicazione minimale che stampa `Hello, world!`, con composition root.
- `api`: API esagonale con router a scelta, un caso d'uso di esempio e storage in memoria.

## Dal checkout del framework

```sh
go run ./cmd/pfw new -template hello-world \
  -module example.com/hello -env dev -framework-dir . /tmp/palma-hello

go run ./cmd/pfw new -template api \
  -module example.com/myapi -env uat -framework-dir . /tmp/palma-api
```

La directory di destinazione deve essere nuova e il suo parent deve esistere.
La CLI rifiuta directory e file già presenti, senza sovrascriverli. I file vengono
prima renderizzati e il codice Go formattato; un errore di scrittura rimuove il
progetto parziale creato dal comando. Non vengono installate dipendenze né avviati
processi durante la creazione.

`-module` è il module path della tua applicazione. `-env` sceglie il primo ambiente
da dichiarare nella composizione e ha default `dev`. Puoi aggiungere altri nomi
in `pfw.Environments(...)` dopo la creazione.

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
  -module example.com/echoapi -env uat -framework-dir . /tmp/palma-echo-api
```

`stdlib` usa `net/http`; `echo` usa Echo v5 e ne aggiunge la dipendenza esplicita
al `go.mod`. Il flag è disponibile soltanto per `api`; nomi non supportati sono
rifiutati prima di creare file. `pfw templates` mostra anche i router disponibili.

I file comuni risiedono in `internal/scaffold/templates/api`. I file specifici
del router sono inclusi da `internal/scaffold/routers/<nome>` e applicati come
variante del trasporto HTTP. Per aggiungere un router in futuro, aggiungere
la sua voce al registro `Routers()` (con le eventuali dipendenze) e i template
di handler e server nella nuova directory. Dominio, application, configurazione,
storage e test del contratto HTTP rimangono condivisi. Gin e Chi non sono
ancora disponibili.

## Avvio e generazione

Entrambi gli skeleton richiedono Go 1.26 o successivo. Il `go.mod` registra la CLI
come tool, così `go tool pfw` usa la stessa versione della libreria.

Per l'API creata sopra:

```sh
cd /tmp/palma-api
go mod tidy
cp .env.example .env
go tool pfw run -env uat ./cmd/api
```

Per hello world:

```sh
cd /tmp/palma-hello
go mod tidy
go tool pfw run -env dev ./cmd/app
```

`run` genera il wiring per l'ambiente selezionato, compila e avvia. Per separare
le operazioni, il progetto include anche una direttiva `go:generate`:

```sh
go generate ./internal/bootstrap
go tool pfw generate -env uat -check ./internal/bootstrap
go test ./...
go build -o bin/api ./cmd/api
PFW_ENV=uat ./bin/api
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
  item/
    domain/                  entità e invarianti
    application/             casi d'uso e porta Repository
    infrastructure/
      http/                  handler, DTO e mapping degli errori
      memory/                implementazione in memoria della porta
```

Dominio e application non importano Palma né il trasporto HTTP. Le implementazioni dell'infrastruttura
dipendono dalle porte dell'application; il composition root sceglie e collega
le implementazioni. Lo storage memory è pronto all'uso e perde i dati al riavvio.
Per aggiungere persistenza, implementare `application.Repository` nell'infrastruttura e sostituire il binding nella composizione.

L'API offre `GET /health`, `GET /items`, `GET /items/{id}` e `POST /items`.
Include decoding JSON con limiti e rifiuto dei campi sconosciuti, validazione
del nome, risposte JSON, errori pubblici senza dettagli interni e test del flusso
HTTP attraverso il wiring generato. Configurazione e file dotenv usano il
prefisso `APP_`; il lifecycle gestisce SIGINT/SIGTERM e shutdown con timeout.

Entrambi i template includono `pfw.Discover("../...")` nella composizione:
la discovery copre tutti i package sotto `internal`. I costruttori dei provider
sono annotati con `//pfw:coconut`; per aggiungere un nuovo provider basta
annotare il suo costruttore, senza mantenere una lista `pfw.Constructors`.
I binding delle interfacce rimangono espliciti, come quello di `Repository`.
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
