# Migration SQL

La CLI include `pfw migrate` per PostgreSQL, senza ORM o dipendenze da strumenti
di migration esterni. Le migration sono operazioni esplicite: `pfw run` non le
esegue automaticamente e non modifica lo schema durante l'avvio dell'API.

## Comandi

```sh
go tool pfw migrate create add_users
go tool pfw migrate status -env staging
go tool pfw migrate up -env staging
go tool pfw migrate up -env prod -steps 1
go tool pfw migrate down -env staging
```

`create` crea un file SQL per versione, con numerazione crescente:

```text
migrations/
  000001_create_items.sql
  000002_add_users.sql
```

Ogni file contiene entrambe le direzioni, separate da marker su righe dedicate:

```sql
-- +pfw Up
CREATE TABLE users (id BIGINT PRIMARY KEY, name TEXT NOT NULL);

-- +pfw Down
DROP TABLE users;
```

I marker devono apparire una sola volta, nell'ordine `Up`, poi `Down`; entrambe
le sezioni devono contenere SQL. Sono ammessi commenti prima di `Up`, ma non SQL
senza sezione. Marker dentro stringhe, commenti a blocco o corpi con dollar quoting
non vengono interpretati come separatori.

Scrivere il SQL nelle due sezioni prima di applicarlo. I placeholder contenenti solo
commenti vengono rifiutati. Il nome deve iniziare con una lettera e usare lettere
minuscole, numeri e underscore. `create` non si connette al database e non richiede
un ambiente; una reservation locale impedisce creazioni concorrenti nella stessa
cartella. Se un processo viene terminato durante la creazione, verificare i file
parziali prima di rimuovere un eventuale `.pfw-create.lock` rimasto sulla directory.

`up` applica tutte le versioni pendenti in ordine numerico; `-steps N` limita il
numero. Ripeterlo non riesegue le migration applicate. `down` esegue le sezioni `Down`
in ordine decrescente e annulla una versione di default; `-steps N` deve essere
positivo e non può superare il numero di versioni applicate. `status` elenca le
versioni come `applied` o `pending`, senza creare la tabella di tracking.

## Configurazione

Il comando cerca la root del modulo anche se invocato da una sottocartella.
In `pfw.toml`:

```toml
env_dir = "./env"
migrations_dir = "./migrations"
```

I percorsi sono relativi al modulo. `-dir` sceglie una cartella alternativa ed è
relativo alla directory corrente. La directory dotenv segue la stessa precedenza
di `run`: `-env-dir`, `PFW_ENV_DIR`, `env_dir` in TOML, root del modulo.

Il comando legge `.env`, `.env.<ambiente>` e `.env.<ambiente>.local` nella directory
selezionata; le variabili del processo prevalgono. L'ambiente si sceglie con `-env`
o `PFW_ENV`. `APP_DB_DSN` deve contenere il DSN del **primary**, non di una replica.
Il backend supportato è PostgreSQL tramite pgx; `APP_DB_DRIVER`, se impostato,
deve essere `pgx`. `APP_DB_CONNECT_TIMEOUT` ha default 5 secondi.

Per un secondo database, scegliere la sua variabile e le sue migration:

```sh
go tool pfw migrate up -env staging -dsn-env BILLING_DB_DSN -dir ./billing/migrations
```

Ogni database fisico ha una propria tabella di tracking. Due directory diverse
non costituiscono namespace indipendenti nello stesso database. In `local` lo
scaffold usa memoria e non configura un DSN SQL: non ci sono migration da eseguire.
Il tool non deduce il backend dal nome dell'ambiente; richiede sempre un DSN.

## Tracking, concorrenza ed errori

`public.pfw_schema_migrations` conserva versione, nome, checksum SHA-256 del SQL delle due
sezioni e timestamp di applicazione. Il comando rifiuta file modificati dopo
l'applicazione, versioni applicate mancanti su disco e nuove versioni inserite
prima di quelle già applicate. Correggere lo schema con una nuova migration,
conservando il contenuto delle precedenti anche quando sono condivise fra ambienti.

Un advisory lock PostgreSQL protegge l'intera operazione su una sessione dedicata.
Ogni migration e il suo record di tracking vengono eseguiti nella stessa
transazione. In caso di errore SQL, quella migration viene annullata; le versioni
precedenti già completate restano applicate. Lo stesso contratto vale per `down`.
Un errore di commit può lasciare incerto l'esito: non vengono effettuati retry
automatici; riconnettersi e controllare `status` prima di decidere come procedere.

Il timeout complessivo, inclusa l'attesa del lock, ha default 5 minuti ed è
configurabile con `-timeout`. SIGINT/SIGTERM cancellano l'operazione. Il rilascio
del lock usa un context di cleanup separato; una sessione con lock dall'esito
incerto viene scartata anziché restituita al pool.

Non inserire `BEGIN`, `COMMIT`, `ROLLBACK` o comandi di transazione nei file.
I blocchi SQL con dollar quoting, come `DO $$ ... $$`, sono supportati. Operazioni
che PostgreSQL non permette in una transazione, per esempio
`CREATE INDEX CONCURRENTLY`, non sono supportate da questo primo backend.
Scrivere esplicitamente il SQL inverso: un rollback può rimuovere dati e non è
deducibile automaticamente dalla sezione `Up`.

Lo scaffold include `000001_create_items.sql` con entrambe le sezioni. Prima dell'avvio
in staging/prod, configurare il relativo DSN e lanciare `pfw migrate up -env ...`.
I test del motore usano un driver locale per verificare tracking, rollback e lock;
non richiedono e non modificano database esterni.
