# API Contract: 对时服务 (NTP Sync)

> Status: confirmed
> Base Path: `/api/ntp`

## 1. Scope & Boundaries

- **Backend**: `app/internal/api/ntp.go`, `app/internal/service/ntp.go`, `app/internal/repository/system_config.go`
- **Frontend**: `ui/apps/web-antd/src/api/ntp.ts`, `ui/apps/web-antd/src/views/ops/time/`
- **Shared Conventions**: 统一包装 `{code: 0, data: ..., message: "ok"}`，非 0 为业务异常

## 2. Common Models

### `NTPConfig`

```json
{
  "id": 1,
  "mode": "ntp",
  "servers": [
    "pool.ntp.org",
    "ntp.aliyun.com"
  ],
  "createdAt": "2025-08-22T08:00:00Z",
  "updatedAt": "2025-08-22T08:00:00Z"
}
```

| Field | Type | Description |
| --- | --- | --- |
| `id` | `number` | 配置 ID |
| `mode` | `string` | 对时模式：`"ntp"` 或 `"manual"` |
| `servers` | `string[]` | NTP 服务器地址列表 |
| `createdAt` | `string` | 创建时间 (RFC3339) |
| `updatedAt` | `string` | 更新时间 (RFC3339) |

### `SyncStatus`

```json
{
  "synced": true,
  "source": "ntp.aliyun.com",
  "offset": "+0.002s",
  "lastSyncTime": "2025-08-22T08:30:00Z"
}
```

| Field | Type | Description |
| --- | --- | --- |
| `synced` | `boolean` | 当前系统时钟是否已完成同步 |
| `source` | `string` | 当前有效同步源 |
| `offset` | `string` | 时钟偏移量 |
| `lastSyncTime` | `string \| null` | 最近一次同步成功时间 (RFC3339) |

---

## 3. Endpoints

### 3.1 获取当前对时配置

- **Method**: `GET`
- **Path**: `/api/ntp/config`
- **Permission**: `ops:time:read`
- **Response**: `Result<NTPConfig>`

  ```json
  {
    "code": 0,
    "data": {
      "id": 1,
      "mode": "ntp",
      "servers": ["pool.ntp.org", "ntp.aliyun.com"],
      "createdAt": "2025-08-22T08:00:00Z",
      "updatedAt": "2025-08-22T08:00:00Z"
    },
    "message": "ok"
  }
  ```

### 3.2 更新对时配置

- **Method**: `PUT`
- **Path**: `/api/ntp/config`
- **Permission**: `ops:time:edit`
- **Request Body**:

  ```json
  {
    "mode": "ntp",
    "servers": ["ntp.aliyun.com", "time.google.com"]
  }
  ```

- **Response**: `Result<null>`

  ```json
  {
    "code": 0,
    "data": null,
    "message": "ok"
  }
  ```

- **Errors**:
  - `1009`: 参数校验失败（mode 取值非法）
  - `1203`: NTP 模式下服务器列表不能为空
  - `1204`: 无效的对时模式

### 3.3 实时获取同步状态

- **Method**: `GET`
- **Path**: `/api/ntp/status`
- **Permission**: `ops:time:read`
- **Response**: `Result<SyncStatus>`

  ```json
  {
    "code": 0,
    "data": {
      "synced": true,
      "source": "ntp.aliyun.com",
      "offset": "+0.002s",
      "lastSyncTime": "2025-08-22T08:30:00Z"
    },
    "message": "ok"
  }
  ```

### 3.4 触发立即同步

- **Method**: `POST`
- **Path**: `/api/ntp/sync`
- **Permission**: `ops:time:edit`
- **Response**: `Result<null>`

  ```json
  {
    "code": 0,
    "data": null,
    "message": "ok"
  }
  ```

- **Errors**:
  - `1202`: 当前处于手动模式，不支持触发 NTP 同步
  - `1206`: NTP 同步失败

### 3.5 手动设置系统时间

- **Method**: `POST`
- **Path**: `/api/ntp/set-time`
- **Permission**: `ops:time:edit`
- **Request Body**:

  ```json
  {
    "time": "2025-08-22T14:30:00Z"
  }
  ```

- **Response**: `Result<null>`

  ```json
  {
    "code": 0,
    "data": null,
    "message": "ok"
  }
  ```

- **Errors**:
  - `1009`: 时间格式不合法（必须是 RFC3339 字符串）
  - `1201`: 当前处于 NTP 模式，不支持手动设置时间
  - `1205`: 系统时间设置失败

### 3.6 内部同步状态查询 (C++ / Webhook 用)

- **Method**: `GET`
- **Path**: `/api/ntp/synced`
- **Auth**: 需要有效 Token，仅要求认证（显式注册 `PermCodeAuthenticated`，不限制具体权限码）
- **Response**: `Result<{ synced: boolean }>`

  ```json
  {
    "code": 0,
    "data": {
      "synced": true
    },
    "message": "ok"
  }
  ```
