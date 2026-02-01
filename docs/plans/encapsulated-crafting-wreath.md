# ASUTP Collector - План реализации

## Обзор

Сервис для сбора данных АСУТП с ГЭС и отправки в центральную систему (prime.speedwagon.uz).

**Ключевые особенности:**
- Работает в изолированной сети ГЭС (PUSH модель)
- Конфигурируемые endpoints через YAML
- Масштабируется для крупных ГЭС без изменения кода
- Kubernetes deployment

---

## 1. Структура проекта

```
asutp/
├── cmd/
│   └── collector/
│       └── main.go
├── config/
│   ├── config.yaml           # Основной конфиг
│   └── stations/
│       └── ges1.yaml         # Конфиг ГЭС-1
├── internal/
│   ├── config/
│   │   ├── config.go         # Структуры конфига
│   │   └── station.go        # Структуры станции
│   ├── collector/
│   │   ├── collector.go      # Интерфейс Collector
│   │   ├── manager.go        # Менеджер сбора
│   │   └── adapters/
│   │       └── energy_api.go # Адаптер текущего API
│   ├── sender/
│   │   ├── sender.go         # HTTP отправитель
│   │   └── retry.go          # Retry логика
│   ├── buffer/
│   │   └── sqlite.go         # Локальный буфер
│   ├── model/
│   │   ├── envelope.go       # Обёртка данных
│   │   └── datapoint.go      # Точка данных
│   ├── health/
│   │   └── server.go         # Health endpoint
│   └── lib/
│       └── logger/sl/sl.go   # slog helper
├── deployments/
│   └── kubernetes/
│       ├── deployment.yaml
│       ├── configmap.yaml
│       └── secret.yaml
├── go.mod
└── Dockerfile
```

---

## 2. Конфигурация

### 2.1. Основной конфиг (`config/config.yaml`)

```yaml
env: prod

station:
  id: "ges1"
  name: "ГЭС-1"
  config_path: "/etc/asutp/stations/ges1.yaml"

sender:
  url: "https://prime.speedwagon.uz/api/v1/asutp/telemetry"
  token: "${SENDER_TOKEN}"
  timeout: 30s
  retry:
    max_attempts: 5
    initial_delay: 1s
    max_delay: 60s

buffer:
  enabled: true
  path: "/var/lib/asutp/buffer.db"
  max_age: 24h

health:
  address: ":8080"

log:
  level: info
  format: json
```

### 2.2. Конфиг станции (`config/stations/ges1.yaml`)

```yaml
station_id: "ges1"
station_name: "ГЭС-1"

connection:
  base_url: "http://192.168.1.249:8200/api/energy/machines"
  adapter: "energy_api"
  timeout: 10s

polling:
  interval: 10s
  timeout: 5s

devices:
  # Линии 35 кВ
  - id: "line_35kv_1"
    name: "Линия 35КВ #1"
    group: "lines_35kv"
    endpoint: "1_35KV"
    request_param: "telemetry"
    fields:
      - {source: "P_kw", target: "active_power_kw", unit: "kW", type: float}
      - {source: "Q_kVar", target: "reactive_power_kvar", unit: "kVar", type: float}
      - {source: "Ia_A", target: "current_a", unit: "A", type: float}
      - {source: "Ib_A", target: "current_b", unit: "A", type: float}
      - {source: "Ic_A", target: "current_c", unit: "A", type: float}
      - {source: "Uab_kV", target: "voltage_ab", unit: "kV", type: float}
      - {source: "cos_phi", target: "power_factor", type: float}

  - id: "line_35kv_2"
    # ... аналогично line_35kv_1

  # Секции шин 10 кВ
  - id: "bus_10kv_1"
    name: "Секция шин 10КВ #1"
    group: "bus_sections"
    endpoint: "10KV1"
    request_param: "telemetry"
    fields:
      - {source: "Uab_kV", target: "voltage_ab", unit: "kV", type: float}
      - {source: "Ubc_kV", target: "voltage_bc", unit: "kV", type: float}
      - {source: "Uca_kV", target: "voltage_ca", unit: "kV", type: float}
      - {source: "F_Hz", target: "frequency", unit: "Hz", type: float}

  # Генераторы
  - id: "gen1_telemetry"
    name: "Гидроагрегат #1"
    group: "generators"
    endpoint: "1_generator_unit"
    request_param: "telemetry"
    fields:
      - {source: "P_kw", target: "active_power_kw", unit: "kW", type: float}
      - {source: "Q_kVar", target: "reactive_power_kvar", unit: "kVar", type: float}
      - {source: "UL_V", target: "rotor_voltage", unit: "V", type: float}
      - {source: "IL_V", target: "rotor_current", unit: "A", type: float}
      - {source: "opening_degree_one", target: "guide_vane_1", unit: "%", type: float}
      - {source: "opening_degree_two", target: "guide_vane_2", unit: "%", type: float}
      # ... остальные поля

  # Трансформаторы
  - id: "transformer_1"
    name: "Трансформатор #1"
    group: "transformers"
    endpoint: "1_MT"
    request_param: "telemetry"
    fields:
      - {source: "Oil_C", target: "oil_temperature", unit: "C", type: float}
      - {source: "Winding_C", target: "winding_temperature", unit: "C", type: float}

  # Состояние выключателей
  - id: "gen1_state"
    name: "Состояние ГА #1"
    group: "breaker_states"
    endpoint: "1_GEN"
    request_param: "telemex"
    fields:
      - {source: "CB_Gen1_Output_On", target: "breaker_on", type: bool}
      - {source: "CB_Gen1_Output_WorkPos", target: "breaker_work_pos", type: bool}

  # Аварийные сигналы
  - id: "gen1_alarms"
    name: "Аварии ГА #1"
    group: "alarms"
    endpoint: "1_GEN_Alarm"
    request_param: "telemex"
    fields:
      - {source: "Gen1_OverSpeed_140_Alarm", target: "overspeed_140", type: bool, severity: critical}
      - {source: "Gen1_Emergency_Stop", target: "emergency_stop", type: bool, severity: critical}
      - {source: "Gen1_Unit_Fire_Alarm", target: "fire_alarm", type: bool, severity: critical}
      # ... все 35 сигналов аварий
```

---

## 3. Модели данных

### 3.1. Envelope (отправляемые данные)

```go
type Envelope struct {
    ID          string      `json:"id"`
    StationID   string      `json:"station_id"`
    StationName string      `json:"station_name"`
    Timestamp   time.Time   `json:"timestamp"`
    DeviceID    string      `json:"device_id"`
    DeviceName  string      `json:"device_name"`
    DeviceGroup string      `json:"device_group"`
    Values      []DataPoint `json:"values"`
}

type DataPoint struct {
    Name     string  `json:"name"`
    Value    any     `json:"value"`
    Unit     string  `json:"unit,omitempty"`
    Quality  string  `json:"quality"`
    Severity string  `json:"severity,omitempty"`
}
```

### 3.2. Пример JSON для srmt-prime

```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "station_id": "ges1",
  "station_name": "ГЭС-1",
  "timestamp": "2026-02-01T12:00:00Z",
  "device_id": "gen1_telemetry",
  "device_name": "Гидроагрегат #1",
  "device_group": "generators",
  "values": [
    {"name": "active_power_kw", "value": 2938.0, "unit": "kW", "quality": "good"},
    {"name": "frequency", "value": 50.03, "unit": "Hz", "quality": "good"},
    {"name": "rotor_voltage", "value": 81.44, "unit": "V", "quality": "good"}
  ]
}
```

---

## 4. Ключевые компоненты

### 4.1. Collector Interface

```go
type Collector interface {
    Collect(ctx context.Context, device *config.DeviceConfig) (*CollectedData, error)
    Name() string
    Close() error
}
```

### 4.2. Sender Interface

```go
type Sender interface {
    Send(ctx context.Context, envelope *model.Envelope) error
    SendBatch(ctx context.Context, envelopes []*model.Envelope) error
    Health(ctx context.Context) error
}
```

### 4.3. Buffer Interface

```go
type Buffer interface {
    Store(ctx context.Context, envelope *model.Envelope) error
    GetPending(ctx context.Context, limit int) ([]*model.Envelope, error)
    MarkSent(ctx context.Context, ids []string) error
    Cleanup(ctx context.Context, maxAge time.Duration) error
}
```

---

## 5. Flow данных

```
┌─────────────────────────────────────────────────────────────┐
│  ASUTP Collector (в сети ГЭС)                               │
│                                                             │
│  time.Ticker (10s)                                          │
│       │                                                     │
│       ▼                                                     │
│  ┌─────────────┐     ┌─────────────┐                       │
│  │  Collector  │────▶│  Transform  │                       │
│  │  (parallel) │     │   + Build   │                       │
│  └─────────────┘     │  Envelope   │                       │
│       │              └──────┬──────┘                       │
│       │                     │                              │
│       │              ┌──────▼──────┐                       │
│       │              │   Sender    │                       │
│       │              │  (+ retry)  │                       │
│       │              └──────┬──────┘                       │
│       │                     │                              │
│       │         ┌───────────┼───────────┐                  │
│       │         │ success   │  failure  │                  │
│       │         ▼           ▼           │                  │
│       │      done      ┌─────────┐      │                  │
│       │                │ SQLite  │      │                  │
│       │                │ Buffer  │──────┘                  │
│       │                └─────────┘  (retry later)          │
│       │                                                    │
└───────┼────────────────────────────────────────────────────┘
        │
        │ HTTPS POST (push)
        ▼
┌─────────────────────────────────────────────────────────────┐
│  prime.speedwagon.uz                                        │
│  POST /api/v1/asutp/telemetry                              │
│  Authorization: Bearer {token}                              │
└─────────────────────────────────────────────────────────────┘
```

---

## 6. API endpoint для srmt-prime

Нужно добавить в srmt-prime:

### 6.1. Handler

```go
// POST /api/v1/asutp/telemetry
func (h *AsutpHandler) ReceiveTelemetry(w http.ResponseWriter, r *http.Request) {
    var envelope model.Envelope
    if err := render.DecodeJSON(r.Body, &envelope); err != nil {
        render.Status(r, http.StatusBadRequest)
        render.JSON(w, r, resp.BadRequest("invalid payload"))
        return
    }

    // Сохранение в БД
    if err := h.repo.SaveTelemetry(r.Context(), &envelope); err != nil {
        // ...
    }

    render.JSON(w, r, resp.OK())
}
```

### 6.2. Таблица PostgreSQL

```sql
CREATE TABLE asutp_telemetry (
    id UUID PRIMARY KEY,
    station_id VARCHAR(50) NOT NULL,
    station_name VARCHAR(255),
    device_id VARCHAR(100) NOT NULL,
    device_name VARCHAR(255),
    device_group VARCHAR(50),
    timestamp TIMESTAMPTZ NOT NULL,
    values JSONB NOT NULL,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_asutp_telemetry_station ON asutp_telemetry(station_id, timestamp);
CREATE INDEX idx_asutp_telemetry_device ON asutp_telemetry(device_id, timestamp);
```

---

## 7. Kubernetes deployment

### 7.1. Deployment

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: asutp-collector-ges1
spec:
  replicas: 1
  selector:
    matchLabels:
      app: asutp-collector
      station: ges1
  template:
    spec:
      containers:
        - name: collector
          image: registry.speedwagon.uz/asutp-collector:latest
          env:
            - name: CONFIG_PATH
              value: /etc/asutp/config.yaml
            - name: SENDER_TOKEN
              valueFrom:
                secretKeyRef:
                  name: asutp-secrets
                  key: sender-token
          volumeMounts:
            - name: config
              mountPath: /etc/asutp
            - name: buffer
              mountPath: /var/lib/asutp
          livenessProbe:
            httpGet:
              path: /health
              port: 8080
            initialDelaySeconds: 5
            periodSeconds: 10
      volumes:
        - name: config
          configMap:
            name: asutp-collector-ges1
        - name: buffer
          persistentVolumeClaim:
            claimName: asutp-buffer-ges1
```

---

## 8. Добавление новой ГЭС

| Сценарий | Действия | Код |
|----------|----------|-----|
| Тот же API формат | Создать `ges2.yaml`, деплой в K8s | Нет |
| Другой API формат | Создать новый адаптер | Да |

Для ГЭС с тем же API (Energy API) - только конфиг:
```bash
cp config/stations/ges1.yaml config/stations/ges2.yaml
# Изменить station_id, base_url, devices
kubectl apply -f deployments/kubernetes/ges2/
```

---

## 9. Файлы для реализации

### Collector
- `internal/config/config.go` - структуры конфига
- `internal/config/station.go` - структуры станции
- `internal/collector/collector.go` - интерфейс
- `internal/collector/manager.go` - менеджер сбора
- `internal/collector/adapters/energy_api.go` - адаптер
- `internal/sender/sender.go` - HTTP sender
- `internal/sender/retry.go` - retry логика
- `internal/buffer/sqlite.go` - локальный буфер
- `internal/model/envelope.go` - модели
- `internal/health/server.go` - health check
- `cmd/collector/main.go` - точка входа

### srmt-prime (новый endpoint)
- `internal/http-server/handlers/asutp/telemetry.go`
- `internal/storage/repo/asutp.go`
- `migrations/postgres/xxx_create_asutp_telemetry.sql`

---

## 10. Технологии

| Компонент | Технология |
|-----------|------------|
| Конфиги | cleanenv + YAML |
| Логирование | log/slog |
| HTTP | chi + render |
| Буфер | SQLite (mattn/go-sqlite3) |
| UUID | google/uuid |
| Retry | custom exponential backoff |

---

## 11. Verification

После реализации проверить:

1. **Сбор данных**: Запустить collector локально, проверить логи сбора
2. **Трансформация**: Убедиться что данные корректно маппятся
3. **Отправка**: Тест с mock сервером
4. **Буферизация**: Отключить сеть, проверить что данные сохраняются в SQLite
5. **Retry**: Проверить что после восстановления сети данные отправляются
6. **Health**: GET /health возвращает статус компонентов
7. **Kubernetes**: Deploy в тестовый namespace, проверить liveness probe
