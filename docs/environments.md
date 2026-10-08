# Ambienti, composizione e avvio

Palma genera il grafo di un solo ambiente per volta. Il nome viene selezionato
in fase di generazione; il file contiene un unico initializer per dichiarazione,
con chiamate Go dirette ai provider del grafo scelto.

```sh
pfw generate -env preview ./internal/bootstrap
pfw generate -env preview -check ./internal/bootstrap
```

`-env` è obbligatorio se le composizioni ricevono `pfw.Environment`. Per gli
initializer senza ambiente, `generate` continua a funzionare senza il flag.
Cambiare ambiente richiede rigenerare il wiring e ricompilare il binario.
`-check` confronta il file con il grafo dell'ambiente specificato e non scrive file.

## Avvio

```sh
go run ./cmd/pfw run -env local ./examples/httpapi/cmd/api
go run ./cmd/pfw run -env staging ./examples/httpapi/cmd/api
```

Con la CLI installata: `pfw run -env local ./cmd/api`. `-env` prevale su `PFW_ENV`;
se entrambi sono assenti il comando richiede una selezione esplicita. I nomi
devono iniziare con una lettera minuscola e contenere lettere minuscole, numeri,
underscore o trattini, senza separatori di percorso.

Il comando individua un solo package `main`, trova i package di composizione
raggiungibili tramite import nel suo modulo, genera il wiring per `-env`, compila
un binario temporaneo e lo esegue dalla root del modulo. Non richiede directory
o nomi di file specifici. Le composizioni situate in altri moduli vanno generate
separatamente. `--` dopo il package permette argomenti applicativi.

```sh
pfw run -env local -env-dir ./configuration ./cmd/api -- -verbose
```

I file sono letti dalla root del modulo, oppure dalla directory specificata da
`-env-dir` (relativa alla directory corrente della CLI):

```text
.env
.env.staging
.env.staging.local
```

Ultimo file prevale sui precedenti; l'environment del processo prevale sui file;
i default delle struct rimangono l'ultima sorgente. I file assenti sono opzionali,
ma errori di lettura o sintassi interrompono l'avvio. La CLI costruisce soltanto
l'environment del processo figlio: non modifica quello del chiamante.
`pfw.toml` può impostare `env_dir = "./env"`, relativo alla root del modulo.
La precedenza per la directory è `-env-dir`, `PFW_ENV_DIR`, `env_dir` in TOML,
poi la root del modulo.

`PFW_ENV` e `PFW_ENV_DIR` sono selettori riservati, impostati dal comando e non
sovrascrivibili tramite dotenv. I valori non vengono stampati dalla CLI.

stdin/stdout/stderr e gli argomenti sono inoltrati all'applicazione. Su Unix
SIGINT/SIGTERM sono inoltrati al binario; un secondo segnale o la scadenza di
`-shutdown-timeout` (default 30s) forza la terminazione. Gli exit code applicativi
sono preservati. Il lifecycle dell'applicazione gestisce lo shutdown ordinato.
L'inoltro programmatico di interrupt su Windows dipende dal supporto del sistema;
il timeout di terminazione resta applicabile.

## Dichiarazioni di composizione

Gli initializer che usano ambienti dichiarano un parametro `pfw.Environment`
(anche attraverso un alias), con nome e posizione liberi:

```go
func Initialize(env pfw.Environment, cfg Config) (*Application, func() error, error) {
    return pfw.BuildWithCleanup[*Application](
        pfw.Environments("dev", "preview", "live"),
        Users,
    )
}
```

I nomi degli ambienti sono scelti dall'applicazione: non esiste un insieme
predefinito. Ogni initializer con un parametro `pfw.Environment` deve dichiarare
gli ambienti supportati attraverso `pfw.Environments(...)`, direttamente o in
un modulo incluso. Ogni `ForEnv` deve riferirsi a uno dei nomi dichiarati e non
aggiunge ambienti implicitamente. Le costanti `pfw.Local`, `pfw.Staging` e
`pfw.Production` sono comodità opzionali, senza comportamenti speciali.
Per inizializzazioni senza parametri ambiente, il comportamento precedente
resta valido; `ForEnv` richiede invece il selettore tipizzato.

Per aggiornare composizioni che usavano gli ambienti impliciti, aggiungere
`pfw.Environments(pfw.Local, pfw.Staging, pfw.Production)` al `Build` o a un
modulo incluso, poi rigenerare il wiring. Per usare nomi personalizzati basta
dichiararli e avviare, per esempio, `pfw run -env preview ./cmd/api`:
il loader selezionerà `.env`, `.env.preview` e `.env.preview.local`.

```go
var Users = pfw.Module(
    pfw.Discover("../user/..."),
    pfw.Implementation[Repository, *postgres.Store](),
    pfw.ForEnv("dev",
        pfw.Implementation[Repository, *memory.Store](),
    ),
)
```

Una discovery ampia può registrare entrambe le implementazioni. Per ogni
ambiente viene costruito soltanto il grafo raggiungibile con i binding selezionati.
Un binding specifico prevale sul comune. Binding diversi per la stessa interfaccia
allo stesso livello sono un errore; identici sono deduplicati. `Bind` e
`Implementation` hanno le stesse regole.

## Override dei provider

```go
var Users = pfw.Module(
    pfw.Discover("../user/application", "../user/adapter/http"),
    pfw.Constructors(postgres.NewRepository),
    pfw.ForEnv("dev",
        pfw.Override(pfw.Discover("../user/adapter/memory")),
    ),
    pfw.AutoBind(),
)
```

`Override` avvolge gruppi di costruttori, discovery o moduli. Le implementazioni
marcate override hanno precedenza fra i candidati compatibili dello scope di
autobinding. Due override compatibili richiedono un binding manuale. I binding
manuali continuano a prevalere. `Constructors` dentro `ForEnv`, senza `Override`,
aggiunge provider ordinari e non risolve implicitamente le ambiguità.

Per un tipo concreto identico, un provider override prevale su quello ordinario.
Per tipi concreti diversi il provider comune rimane disponibile se richiesto
direttamente: sostituire un repository attraverso un'interfaccia non rimuove
automaticamente tutte le dipendenze da `*postgres.Store`.

La discovery dentro un ramo contribuisce solamente al grafo di quell'ambiente;
le funzioni scoperte e registrate manualmente rimangono deduplicate. Scope di
`AutoBind`, esclusioni e cleanup sono calcolati dopo la selezione dei rami.
La generazione risolve e verifica soltanto il grafo dell'ambiente selezionato.
Le dichiarazioni dei nomi vengono comunque validate, inclusi i riferimenti
`ForEnv` ad ambienti non dichiarati. Il codice emesso contiene soltanto i provider
raggiungibili nel grafo scelto. Gli altri rami devono essere verificati con
ulteriori generazioni o con `inspect`, che continua a validare tutti i grafi.

## Configurazione e validazione

```go
cfg, err := pfwconfig.Load[Config](pfwconfig.Options{
    Environment: env,
    EnvDir: directory,
    Prefix: "APP_",
})
```

Con `Environment` e senza `EnvFiles` espliciti, il loader usa automaticamente
i tre file del profilo. Un elenco `EnvFiles` esplicito mantiene il controllo
sui percorsi e la normale opzione `IgnoreMissingEnvFiles`.
`Validate()` verifica i vincoli comuni; un eventuale `ValidateEnvironment(pfw.Environment)`
viene chiamato successivamente per quelli condizionali. Per esempio DB.DSN
può essere obbligatorio in staging/production e assente in locale.
I campi condizionalmente richiesti non devono avere `envRequired:"true"` globale.

Un binario può funzionare senza la CLI: l'applicazione seleziona l'ambiente con
`pfw.EnvironmentFromEnv(fallback)`, carica/valida la configurazione e passa lo
stesso ambiente all'initializer. Un ambiente diverso da quello usato per
la generazione viene rifiutato prima di eseguire costruttori, anche se dichiarato
in `Environments`. La CLI non può rendere automaticamente
environment-aware un'applicazione che usa un proprio loader indipendente.

## Ispezione

```sh
pfw inspect -env local ./internal/bootstrap
pfw inspect -env production ./internal/bootstrap
```

Senza filtro vengono mostrati tutti gli ambienti. Il JSON espone `environment`
per ogni initializer e `override` sui provider corrispondenti. Il piano e il
cleanup riflettono soltanto i rami attivi. La validazione continua a controllare
tutti i grafi. Gli initializer senza ambiente rimangono visibili con qualsiasi filtro.
