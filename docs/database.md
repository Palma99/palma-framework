# Connessioni al database

Il package `database` standardizza apertura, configurazione e cleanup dei pool
SQL. I repository ricevono il database attraverso il costruttore e scelgono
esplicitamente il pool. Non esistono variabili globali, container runtime, query
builder, routing delle query, retry o migrazioni implicite.

## Un database e un provider

L'applicazione importa il driver che intende usare, per esempio
`_ "github.com/jackc/pgx/v5/stdlib"`. Palma usa `database/sql` senza importare
driver specifici nel componente. Una `Connection` contiene un pool primary e
zero o più pool replica; non rappresenta una singola connessione fisica.

```go
// Nel package di infrastruttura dell'applicazione.
type MainDB struct{ *database.Connection }

//pfw:coconut
func OpenMainDB(ctx context.Context, cfg Config) (MainDB, func() error, error) {
    connection, cleanup, err := database.Open(ctx, database.Config{
        Primary: database.Endpoint{
            Driver: "pgx",
            DSN: cfg.MainDSN,
            ConnectTimeout: 5 * time.Second,
            Pool: &database.PoolConfig{
                MaxOpenConns: 20,
                MaxIdleConns: 5,
                ConnMaxLifetime: 30 * time.Minute,
                ConnMaxIdleTime: 5 * time.Minute,
            },
        },
    })
    return MainDB{Connection: connection}, cleanup, err
}
```

Gli import sono `context`, `time` e
`github.com/palma99/palma-framework/database`; `Config` è la configurazione
dell'applicazione caricata prima dell'inizializzazione. `MainDSN` è un suo campo.
Non servono un file di configurazione aggiuntivo o un secondo loader.

Registrare il provider tramite `pfw.Constructors(OpenMainDB)` oppure includere
il suo package nella discovery. Usare `BuildWithCleanup` nel composition root e
passare il cleanup al lifecycle. Il grafo costruisce il provider una sola volta
per inizializzazione; tutti i repository che richiedono `MainDB` condividono il
medesimo gruppo. Un provider non raggiungibile non apre pool.

## Più database e repliche

Per un secondo database dichiarare un tipo distinto, per esempio
`type BillingDB struct{ *database.Connection }`, e un provider corrispondente.
Il costruttore `NewInvoiceRepository(db BillingDB)` riceve il database billing;
`NewUserRepository(db MainDB)` riceve quello principale. Un repository può
richiederli entrambi. La scelta avviene tramite i tipi del grafo Go, senza
qualificatori o lookup per nome del database nei repository.

Le repliche appartengono invece a un singolo gruppo e hanno nomi espliciti:

```go
connection, cleanup, err := database.Open(ctx, database.Config{
    Primary: database.Endpoint{Driver: "pgx", DSN: cfg.MainDSN},
    Replicas: map[string]database.Endpoint{
        "report": {Driver: "pgx", DSN: cfg.ReportDSN},
        "search": {Driver: "pgx", DSN: cfg.SearchDSN},
    },
})
```

Ogni endpoint ha limiti e timeout indipendenti. Il componente non verifica né
configura la replicazione: l'applicazione deve fornire endpoint del database
corretto. I nomi sono case-sensitive e non devono essere vuoti o solo spazi.

## Due metodi dello stesso repository

```go
type UserRepository struct{ db MainDB }

func NewUserRepository(db MainDB) *UserRepository {
    return &UserRepository{db: db}
}

// Una scrittura usa sempre il primary.
func (r *UserRepository) UpdateName(ctx context.Context, id int64, name string) error {
    executor, err := sqltx.Executor(ctx, r.db.Primary())
    if err != nil {
        return err
    }
    _, err = executor.ExecContext(ctx,
        "UPDATE users SET name = $1 WHERE id = $2", name, id)
    return err
}

// La scelta avviene a runtime: fresh richiede una lettura dal primary.
func (r *UserRepository) Count(ctx context.Context, fresh bool) (int64, error) {
    pool := r.db.Primary()
    if !fresh {
        replica, err := r.db.Replica("report")
        if err != nil {
            return 0, err
        }
        pool = replica
    }
    executor, err := sqltx.Executor(ctx, pool)
    if err != nil {
        return 0, err
    }
    var count int64
    err = executor.QueryRowContext(ctx, "SELECT COUNT(*) FROM users").Scan(&count)
    return count, err
}
```

L'import `sqltx` è `github.com/palma99/palma-framework/transaction/sql`.
`Primary()` restituisce `*sql.DB`; `Replica(name)` restituisce `(*sql.DB, error)`.
Una replica mancante restituisce `database.ErrReplicaNotFound`, verificabile con
`errors.Is`. Non esiste fallback implicito al primary o selezione casuale di
un'altra replica. Senza transazioni, si possono usare direttamente le API dei
pool, per esempio `r.db.Primary().ExecContext(...)`.

`Count(ctx, false)` accetta il ritardo di replica; `Count(ctx, true)` legge dal
primary. All'interno di una transazione sul primary occorre scegliere il primary:
`sqltx.Executor` restituisce la transazione condivisa. Una replica o un database
diverso produce `sqltx.ErrDifferentDatabase`; un context transazionale scaduto
produce errore. Il componente database non modifica queste regole e non deduce
la destinazione dal testo SQL. Vedi la [guida alle transazioni](transactions.md).

## Avvio, default e ownership

`Open` valida tutti gli endpoint prima di aprire risorse, poi apre e verifica
il primary e le repliche in ordine di nome tramite `PingContext`. Un errore su
qualsiasi endpoint impedisce l'avvio, chiude i pool già acquisiti in ordine
inverso e restituisce connessione e cleanup nil. Il timeout di ogni endpoint
include connessione e ping e rispetta anche la cancellazione del context iniziale.

- `ConnectTimeout: 0` usa il default di 5 secondi; valori negativi sono invalidi.
- `Pool: nil` mantiene i default di `database/sql`.
- Con `Pool` esplicito, `MaxOpenConns: 0` significa nessun limite,
  `MaxIdleConns: 0` disabilita le connessioni inattive e le durate zero disabilitano
  la relativa scadenza. Valori negativi e un limite idle superiore a un limite
  open positivo sono invalidi.
- Il driver è obbligatorio; il DSN segue il formato del driver, che può anche
  ammettere un DSN vuoto. Nessun DSN viene stampato dal componente negli errori
  di apertura o ping; la causa del driver resta accessibile con `errors.Is/As`.

Il cleanup restituito da `Open` possiede tutti i pool, aggrega gli errori e viene
eseguito una sola volta, anche con chiamate concorrenti; le successive restituiscono
lo stesso risultato. Eseguirlo dopo l'arresto dei consumer. I repository non
devono chiamare `Close` sui pool ricevuti. Il componente non chiude automaticamente
il database quando scade il context usato durante l'avvio.

L'esempio HTTP usa `internal/platform/database.OpenMainDatabase` e inietta
`MainDB` nel repository Postgres. I suoi metodi scelgono sempre il primary.
I test usano driver locali e non richiedono database esterni. Client non SQL,
come MongoDB, richiedono adapter dedicati e non rientrano in questa API.
