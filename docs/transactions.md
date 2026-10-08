# Transazioni applicative

`transaction.Runner` definisce un confine applicativo, utilizzabile da servizi,
use case e job. Il callback può chiamare qualunque funzione; non è limitato ai
repository. L'atomicità delle operazioni dipende dal backend transazionale.

```go
err := transactions.Within(ctx, func(ctx context.Context) error {
    order, err := orders.Create(ctx, input)
    if err != nil {
        return err
    }
    if err := inventory.Reserve(ctx, order); err != nil {
        return err
    }
    return outbox.Append(ctx, OrderCreated{ID: order.ID})
})
```

Passare sempre il context ricevuto dal callback alle operazioni e agli altri
use case. Conserva valori, deadline e cancellazione del chiamante e trasporta
lo scope transazionale. Non usare `context.Background()` per operazioni che
devono partecipare alla transazione.

## Backend SQL

`github.com/palma99/palma-framework/transaction/sql`, importato come `sqltx`,
usa `database/sql` e non dipende da un particolare driver:

```go
//pfw:coconut
func NewTransactions(db *sql.DB) (*transaction.Manager, error) {
    return sqltx.New(db, sqltx.Options{
        Isolation: sql.LevelReadCommitted,
    })
}
```

Un servizio può dichiarare una porta locale con la firma di `Within`, oppure
ricevere `transaction.Runner`. `*transaction.Manager` implementa il contratto;
si può selezionarlo con `AutoBind` o con un binding esplicito.
Condividere la stessa istanza di manager per lo stesso confine applicativo.
Non ci sono manager globali né provider di fallback che simulano una transazione.

I repository scelgono l'executor tramite il context:

```go
func (r *Repository) Save(ctx context.Context, item Item) error {
    db, err := sqltx.Executor(ctx, r.db)
    if err != nil {
        return err
    }
    _, err = db.ExecContext(ctx, "INSERT INTO items (name) VALUES ($1)", item.Name)
    return err
}
```

Dentro il callback tutti gli adapter che usano lo stesso `*sql.DB` ricevono
lo stesso `*sql.Tx`. Fuori da uno scope ricevono il pool originale. Il repository
Postgres dell'esempio HTTP usa già questo meccanismo.

Un altro database o un backend incompatibile nello scope produce
`sqltx.ErrDifferentDatabase`, senza eseguire l'operazione fuori transazione.
Un executor ottenuto nello scope non va conservato oltre il callback. Anche
le righe e gli statement devono essere consumati/chiusi prima di terminarlo.

Per restituire un risultato:

```go
order, err := transaction.Do(ctx, transactions,
    func(ctx context.Context) (Order, error) {
        return orders.Create(ctx, input)
    },
)
```

Il risultato è azzerato su errore, incluso un commit fallito. Se usato dentro
un altro `Within`, il risultato è provvisorio finché il confine esterno non
termina con successo.

## Commit, rollback e annidamento

- Il livello esterno apre e chiude la transazione.
- Chiamate annidate sullo stesso manager partecipano allo scope esistente.
  Non aprono una seconda transazione e non creano savepoint.
- Un errore annidato rende lo scope rollback-only anche se il chiamante lo
  intercetta. Il confine esterno restituisce `ErrRollbackOnly` e conserva la causa.
- Un panic provoca il tentativo di rollback e viene propagato invariato.
  Anche un panic annidato recuperato dall'applicazione rende lo scope rollback-only.
- Errori del callback, di commit e di rollback sono wrappati o aggregati,
  mantenendo `errors.Is` e `errors.As`. Durante un panic viene conservato il
  valore originale del panic; un eventuale errore di rollback non lo sostituisce.
- Un manager diverso nello scope viene rifiutato con `ErrDifferentManager` e
  rende il confine esterno rollback-only. Non si coordinano commit indipendenti.
- Dopo il callback lo scope è chiuso e il context derivato viene cancellato.
  Riutilizzarlo restituisce `ErrClosed`; non c'è fallback silenzioso al database.

Non esistono retry automatici: ripetere un callback può duplicare effetti esterni.
Un errore di commit può lasciare incerto l'esito effettivo; non implica sempre
che il database abbia annullato tutte le modifiche.

## Context e cleanup

Begin e callback seguono il context del chiamante. Se è già cancellato, non
viene aperta una transazione. Se il callback termina senza errore ma il context
è cancellato, il manager tenta il rollback anziché iniziare il commit.

Il cleanup usa `context.WithoutCancel` per conservare i valori senza ereditare
una cancellazione che impedirebbe il rollback. Aggiunge un timeout, predefinito
5 secondi, configurabile con `transaction.Options.RollbackTimeout` oppure
`sqltx.Options.RollbackTimeout`.
I backend devono rispettare il context ricevuto per rendere effettivo il limite.

`database/sql` lega la transazione al context di `BeginTx` e può annullarla
automaticamente quando viene cancellato. `Commit` e `Rollback` non accettano
un context: l'adapter verifica la cancellazione prima del commit e considera
`sql.ErrTxDone` nel rollback come cleanup già concluso. Il timeout del manager
non può interrompere un `sql.Tx.Rollback` bloccato dal driver.
Riferimenti: [database/sql](https://pkg.go.dev/database/sql#DB.BeginTx) e
[context.WithoutCancel](https://pkg.go.dev/context#WithoutCancel).

Il callback deve attendere tutte le proprie operazioni prima di ritornare.
Il manager può essere condiviso tra richieste indipendenti; questo non rende
una singola connessione transazionale adatta a operazioni concorrenti illimitate.
Deadline derivate più brevi restano valide nelle chiamate annidate; propagare
sempre il context ricevuto, senza rimuoverne la cancellazione.

## Altri tipi di operazioni

Il nucleo non importa SQL. Un backend implementa:

```go
type Beginner interface {
    Begin(context.Context) (transaction.Transaction, error)
}

type Transaction interface {
    Commit(context.Context) error
    Rollback(context.Context) error
}
```

Gli adapter di una risorsa possono ottenere la transazione con
`transaction.Current(ctx)`. `ErrNoTransaction` indica che non c'è uno scope;
`ErrClosed` indica che uno scope esiste ma è terminato. Un backend che fallisce
in Begin deve liberare eventuali risorse parzialmente acquisite.

Calcoli, validazioni e chiamate a servizi possono essere parte del callback.
Soltanto gli effetti che partecipano al backend condividono il suo commit e
rollback. Una scrittura diretta tramite `*sql.DB`, ignorando l'executor, non
partecipa alla transazione.

Per email, HTTP o messaggi già inviati, il rollback del database non annulla
l'effetto remoto. Per consegna successiva al commit, salvare un evento outbox
nella stessa transazione e inviarlo con un worker con retry e idempotenza.
Per processi su risorse indipendenti occorrono compensazioni/saga o un protocollo
transazionale comune: questo componente non implementa quelle garanzie.
