# Operations

## Prometheus metrics

Enable the `/metrics` endpoint:

```sh
export POUR_METRICS=true
pour serve
```

Or in a systemd unit or container, pass `--metrics` (env var takes precedence).

### Available metrics

| Metric                            | Type      | Labels                    | Description                                          |
|-----------------------------------|-----------|---------------------------|------------------------------------------------------|
| `pour_drips_total`                | Counter   | `chain_id`, `status`      | Total drip outcomes: `confirmed`, `queued`, `failed` |
| `pour_requests_total`             | Counter   | `chain_id`, `result`      | HTTP-level admission results                         |
| `pour_batch_size_recipients`      | Histogram | `chain_id`                | Recipients per `MsgMultiSend` batch                  |
| `pour_queue_depth`                | Gauge     | `chain_id`, `distributor` | Current per-distributor queue depth                  |
| `pour_chain_suspended`            | Gauge     | `chain_id`                | `1` while chain is suspended, `0` otherwise          |
| `pour_multisend_disabled_total`   | Counter   | `chain_id`                | Times `MsgMultiSend` was disabled for a chain        |
| `pour_distributor_recovery_total` | Counter   | `chain_id`                | Times a distributor entered recovery mode            |

### Scrape config (Prometheus)

```yaml
scrape_configs:
  - job_name: pour
    static_configs:
      - targets: ["localhost:8080"]
    metrics_path: /metrics
```

---

## Log levels

```sh
export POUR_LOG_LEVEL=debug   # debug | info | warn | error
```

Structured JSON logs are emitted to stderr. At `info` level, each drip request produces one log
line including `chain_id`, `address`, `mechanism`, and `tx_hash` (when confirmed).

At `debug` level, every gRPC call, fee estimation step, and batch flush is logged.

---

## Multiple distributors

Distributors reduce sequence-number contention. Each distributor has its own signing key and
sequence counter and can broadcast in parallel with other distributors.

```yaml
chains:
  - chain_id: osmosis-1
    distributors: 3          # keys 1, 2, 3 — key 0 is the holder
    batch_window: "5s"
    refill_threshold: "50000000uosmo"
```

The holder (key 0) receives all external funding and periodically refills distributor accounts.
Pour balances load across healthy distributors, skipping any in recovery.

**Key derivation:** keys are derived from `POUR_MNEMONIC` using the BIP44 path
`m/44'/118'/0'/0/N` where `N` is the key index. Index 0 is the holder; indices 1..N are
distributors.

Only the holder (key index 0) needs to be funded before starting. The refill loop runs
immediately at startup and tops up each distributor from the holder automatically.

---

## Suspension and recovery

A chain is automatically suspended after **5 consecutive send failures**. While suspended:

- All drip requests return `503 Service Unavailable`
- The `pour_chain_suspended` metric is set to `1`
- An error log is emitted with the suspension reason

Resume the chain after diagnosing the root cause:

```sh
curl -X POST http://localhost:8080/admin/chains/osmosis-1/resume \
  -H "Authorization: Bearer $(cat .pour-admin-token)"
```

Common suspension causes:

- Faucet wallet balance exhausted
- Chain node unreachable (all endpoints unhealthy)
- Persistent sequence mismatch (typically caused by external transactions from the faucet key)

### MsgMultiSend fallback

If `MsgMultiSend` fails 3 times in a row for a chain, pour disables it and falls back to sending
individual `MsgSend` transactions. This is tracked by the `pour_multisend_disabled_total` metric.
The state resets when the chain is resumed.

---

## Database

Drip records are stored in a SQLite database (`pour.db` by default). The database is used to:

- Track the per-address daily cap across requests
- Record drip status transitions (`queued → confirmed`, `queued → failed`)

Set `POUR_DB_PATH` to use a different path. The database is created automatically on first run.

---

## Health check

```sh
curl http://localhost:8080/health
# → {"status":"ok"}
```

Returns `200` as long as the HTTP server is up. Useful as a liveness probe in Kubernetes or
similar environments. Does not verify chain connectivity or wallet balance.
