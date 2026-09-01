# 人员人脸注册与多脸管理设计方案

## 1. 架构与端到端交互流

### 1.1 总体架构

本方案在现有系统（Go 后端 `app/`、C++20 推理引擎 `engine/`、macOS CoreML 算法包 `face_recognition`、Vue3 前端 `ui/`）中引入完整的人员人脸注册与多样本管理链路。

```
+---------------------------------------------------------------------------------------+
|                                    Vue 3 前端 (Web)                                   |
|   - 人员列表展示人脸样本数 (faceCount)                                                  |
|   - 人脸样本管理抽屉/弹窗 (FaceManagementDrawer): 样本网格、原图/特写预览、上传、删除    |
+---------------------------------------------------------------------------------------+
                                        │  ▲ (HTTPS / REST JSON + Inline Image Stream)
                                        ▼  │
+---------------------------------------------------------------------------------------+
|                                    Go 后端 (app/)                                     |
|  [API Layer]                                                                          |
|   - POST   /api/person/:personId/faces            (注册新样本，multipart/form-data)    |
|   - GET    /api/person/:personId/faces            (查询人脸样本摘要列表，不含向量)      |
|   - DELETE /api/person/:personId/faces/:faceId    (删除单个人脸样本)                    |
|   - GET    /api/person/:personId/faces/:faceId/image (受保护的原图文件流)              |
|   - GET    /api/person/:personId/faces/:faceId/aligned-image (受保护的112x112特写流)   |
|  [Service & Domain Layer]                                                             |
|   - 图片 MIME/大小前置校验 (JPG/PNG/WebP, <= 10MiB)                                   |
|   - SHA-256 全局精准去重检测                                                         |
|   - 每个人员未删除人脸样本上限并发控制 (<= 10)                                         |
|   - gRPC 调用 Engine::PersonService.ExtractFaceFeature                                |
|   - 受保护私有存储写入 (原图 + 标准化图) 与 DB 写入失败事务补偿删除                   |
|   - 人员删除时级联软删人脸样本并成对清理私有存储文件                                   |
|  [Storage Layer]                                                                      |
|   - FileStorage 接口扩展 Get / Delete 能力 (local 与 minio 实现对上层透明)            |
|   - 专属私有 key 空间: person-faces/{personId}/{sampleId}_orig.{ext}                   |
|                      person-faces/{personId}/{sampleId}_aligned.jpg                   |
+---------------------------------------------------------------------------------------+
                                        │  ▲ (UDS gRPC / person.proto)
                                        ▼  │
+---------------------------------------------------------------------------------------+
|                                C++ 推理引擎 (engine/)                                 |
|  [PersonServiceImpl::ExtractFaceFeature]                                              |
|   - 请求体边界校验 (字节流 <= 10MB)                                                   |
|   - 平台静态图片解码 (ImageIO -> RGB24，宽高 <= 3840x2160，总像素 <= 8,294,400)      |
|   - 调度活跃的 face_recognition 算法包执行推理：                                      |
|       1. SCRFD 人脸检测与 5 关键点定位                                                |
|       2. 单人脸判定 (0 张 -> NO_FACE_DETECTED, >1 张 -> MULTIPLE_FACES_DETECTED)      |
|       3. 质量门槛校验 (score >= 0.50, min_dim >= 40px, quality >= 35.0)             |
|       4. 五点相似变换几何对齐生成 112x112 RGB 特写图                                  |
|       5. GLINTR100 模型推理提取 512 维特征向量，L2 归一化 (512 float32 / 2048 字节)   |
|       6. 112x112 人脸特写高质量编码为 JPEG 字节流                                     |
|   - 返回结构化结果：embedding raw bytes、aligned jpeg bytes、bbox、quality_score      |
+---------------------------------------------------------------------------------------+
```

---

## 2. 契约定义与 IPC 设计

### 2.1 Proto 契约扩展 (`engine/proto/argus/v1/person.proto`)

在 `person.proto` 中扩展 `ExtractFaceFeature` RPC 与相关结构体，保持原有 `SyncPersons` 签名不变：

```protobuf
syntax = "proto3";

package argus.v1;

option go_package = "argus/app/internal/proto/argus/v1;argusv1";

// 归一化矩形框 [0.0, 1.0]
message NormalizedRect {
  float x = 1;
  float y = 2;
  float width = 3;
  float height = 4;
}

// 人脸特征提取请求
message ExtractFaceFeatureRequest {
  bytes image_data = 1;        // 单张图片原始字节（最大 10MB）
  string mime_type = 2;        // 提示 MIME 类型（如 image/jpeg, image/png, image/webp）
}

// 人脸特征提取响应
message ExtractFaceFeatureResponse {
  string code = 1;             // 稳定错误码，空串表示成功
  string error_message = 2;    // 诊断日志（调用方不得解析其文本）
  bytes embedding = 3;         // 512 维 L2 归一化 float32 小端字节流 (精确 2048 字节)
  bytes aligned_face_image = 4;// 112x112 标准化人脸对齐 JPEG 图片字节流
  NormalizedRect face_box = 5; // 原图中人脸归一化坐标
  float quality_score = 6;     // 综合人脸质量得分 (0.0 ~ 100.0)
  float detection_score = 7;   // 人脸检测置信度 (0.0 ~ 1.0)
  string algorithm_id = 8;     // 算法包标识 (如 "face_recognition")
  string algorithm_version = 9;// 算法包版本号 (如 "1.0.0")
}

// 人员档案（预留）
message PersonRecord {
  string person_id = 1;
  string name = 2;
  string feature_version = 3;
}

message SyncPersonsRequest {
  repeated PersonRecord persons = 1;
}

message SyncPersonsResponse {
  int32 total_indexed = 1;
  string code = 2;
  string error_message = 3;
}

// PersonService: 运行在 C++ 引擎（engine.sock），供 Go 控制面调用
service PersonService {
  // 预留人脸库同步 RPC
  rpc SyncPersons(SyncPersonsRequest) returns (SyncPersonsResponse);

  // 一次性静态图片单人脸特征提取与标准化切图
  rpc ExtractFaceFeature(ExtractFaceFeatureRequest) returns (ExtractFaceFeatureResponse);
}
```

### 2.2 Engine IPC 稳定错误码定义

Engine `PersonServiceImpl` 返回的稳定响应 `code`（遵循 IPC 错误处理规范，Go 侧仅根据 Code 分支，不解析 error_message）：

| Code | 触发条件 | HTTP 映射 | 说明 |
| :--- | :--- | :--- | :--- |
| `""` (空串) | 提取成功且通过单脸及质量检验 | 200 OK | 成功 |
| `IMAGE_TOO_LARGE` | 请求体字节数超出 10MB 或解码分辨率超出限制 | 400 Bad Request | 图片过大 |
| `IMAGE_DECODE_FAILED` | 图片损坏或格式不受平台 ImageIO 支持 | 400 Bad Request | 解码失败 |
| `ALGORITHM_NOT_AVAILABLE` | `face_recognition` 算法包未安装、未激活或加载失败 | 503 Unavailable | 算法不可用 |
| `NO_FACE_DETECTED` | 图片中未检测到任何人脸 (检测到 0 个人脸) | 400 Bad Request | 无合格人脸 |
| `MULTIPLE_FACES_DETECTED` | 图片中检测到多于 1 张人脸 (检测到 >= 2 个人脸) | 400 Bad Request | 存在多张人脸 |
| `FACE_TOO_SMALL` | 人脸短边小于 40px | 400 Bad Request | 人脸尺寸过小 |
| `FACE_QUALITY_TOO_LOW` | 人脸综合质量评分低于 35.0 或检测置信度 < 0.50 | 400 Bad Request | 人脸质量不达标 |
| `INTERNAL_ERROR` | 引擎内部未捕获异常或推理故障 | 500 Internal Error | 内部错误 |

---

## 3. C++ 引擎与算法包实现细节

### 3.1 静态图片解码与处理

在 macOS 平台上，利用 Engine 已经链接的 `ImageIO` 和 `CoreGraphics` 实现内存安全的高性能静态图片字节流解码与标准化 JPEG 编码：

1. **解码**：
   - 使用 `CGDataProviderCreateWithData` + `CGImageSourceCreateWithDataProvider` 从内存字节流解析图片；
   - 提取图片尺寸并校验：`width >= 320 && height >= 320 && width <= 3840 && height <= 2160 && (width * height) <= 8294400`；
   - 使用 `CGBitmapContextCreate` 将 `CGImageRef` 渲染到紧凑的 24-bit RGB (或 32-bit RGBA) 连续内存缓冲区。
2. **算法包单脸检测与特征提取**：
   - 调度活跃的 `face_recognition` 算法包（复用其 CoreML SCRFD 和 GLINTR 推理上下文）；
   - 执行人脸检测与 5 关键点定位；
   - 单脸判定与过滤；
   - 执行五点相似变换对齐人脸为 `112x112` RGB 图像；
   - 调用 GLINTR 模型提取 512 维特征向量，校验所有浮点数均为有限值（`std::isfinite`），计算 L2 范数并归一化；
   - 将归一化后的 512 个 `float` 按照 little-endian 序列化为 2048 字节的 raw bytes；
3. **标准化切图编码**：
   - 使用 `CGImageDestinationCreateWithData` 将 112x112 RGB 渲染图编码为高质量 JPEG（quality = 90%）；
   - 将 JPEG 字节流封装入 `aligned_face_image` 字段返回。

---

## 4. 存储层抽象扩展 (`storage.FileStorage`)

### 4.1 接口扩展与受保护存储

在 `argus/internal/pkg/storage/storage.go` 中，为 `FileStorage` 补充只读与删除能力：

```go
type FileStorage interface {
    // Put 保存文件对象
    Put(ctx context.Context, input PutInput) (StoredObject, error)
    // Get 读取文件对象流，返回 ReadCloser、大小、Content-Type 及错误
    Get(ctx context.Context, key string) (io.ReadCloser, int64, string, error)
    // Delete 幂等删除指定 key 的文件，若文件不存在返回 nil
    Delete(ctx context.Context, key string) error
}
```

### 4.2 Key 路径与隔离原则

人脸图片属于敏感生物特征凭据，严禁使用公开 `/uploads/` 前缀直接对外暴露。
Key 命名约定：
- 原始上传图：`person-faces/{personId}/{sampleId}_orig{ext}`（例如 `person-faces/P001/01HXYZ..._orig.jpg`）
- 标准化特写图：`person-faces/{personId}/{sampleId}_aligned.jpg`

前端与外部客户端无法通过静态文件服务器直接访问该路径，必须通过经过 Bearer Token 认证与鉴权的后端 API 流式拉取。

---

## 5. 数据库设计与数据模型

### 5.1 数据表结构 (`person_face_samples`)

创建 `person_face_samples` 表保存人脸样本元数据与特征张量：

```sql
CREATE TABLE IF NOT EXISTS person_face_samples (
    id BIGSERIAL PRIMARY KEY,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at BIGINT NOT NULL DEFAULT 0,
    sample_id VARCHAR(64) NOT NULL,
    person_id VARCHAR(64) NOT NULL,
    file_sha256 VARCHAR(64) NOT NULL,
    original_image_key VARCHAR(255) NOT NULL,
    aligned_face_key VARCHAR(255) NOT NULL,
    algorithm_id VARCHAR(64) NOT NULL,
    algorithm_version VARCHAR(32) NOT NULL,
    feature_dimension INT NOT NULL DEFAULT 512,
    embedding BYTEA NOT NULL,
    detection_score REAL NOT NULL,
    quality_score REAL NOT NULL,
    bbox_x REAL NOT NULL,
    bbox_y REAL NOT NULL,
    bbox_width REAL NOT NULL,
    bbox_height REAL NOT NULL
);

-- 索引设计
CREATE UNIQUE INDEX IF NOT EXISTS uk_person_face_samples_sample_id ON person_face_samples(sample_id, deleted_at);
CREATE UNIQUE INDEX IF NOT EXISTS uk_person_face_samples_sha256 ON person_face_samples(file_sha256, deleted_at);
CREATE INDEX IF NOT EXISTS idx_person_face_samples_person_id ON person_face_samples(person_id, deleted_at);
```

### 5.2 GORM 模型 (`argus/internal/model/person_face_sample.go`)

```go
package model

import (
    "gorm.io/plugin/soft_delete"
)

// PersonFaceSample 人脸样本与特征向量模型。
type PersonFaceSample struct {
    BaseModel
    DeletedAt        soft_delete.DeletedAt `gorm:"column:deleted_at;softDelete:milli;default:0;uniqueIndex:uk_person_face_samples_sample_id;uniqueIndex:uk_person_face_samples_sha256;index:idx_person_face_samples_person_id" json:"-"`
    SampleID         string                `gorm:"column:sample_id;size:64;not null;uniqueIndex:uk_person_face_samples_sample_id" json:"sampleId"`
    PersonID         string                `gorm:"column:person_id;size:64;not null;index:idx_person_face_samples_person_id" json:"personId"`
    FileSHA256       string                `gorm:"column:file_sha256;size:64;not null;uniqueIndex:uk_person_face_samples_sha256" json:"fileSha256"`
    OriginalImageKey string                `gorm:"column:original_image_key;size:255;not null" json:"-"`
    AlignedFaceKey   string                `gorm:"column:aligned_face_key;size:255;not null" json:"-"`
    AlgorithmID      string                `gorm:"column:algorithm_id;size:64;not null" json:"algorithmId"`
    AlgorithmVersion string                `gorm:"column:algorithm_version;size:32;not null" json:"algorithmVersion"`
    FeatureDimension int                   `gorm:"column:feature_dimension;not null;default:512" json:"featureDimension"`
    Embedding        []byte                `gorm:"column:embedding;type:blob;not null" json:"-"`
    DetectionScore   float32               `gorm:"column:detection_score;not null" json:"detectionScore"`
    QualityScore     float32               `gorm:"column:quality_score;not null" json:"qualityScore"`
    BBoxX            float32               `gorm:"column:bbox_x;not null" json:"bboxX"`
    BBoxY            float32               `gorm:"column:bbox_y;not null" json:"bboxY"`
    BBoxWidth        float32               `gorm:"column:bbox_width;not null" json:"bboxWidth"`
    BBoxHeight       float32               `gorm:"column:bbox_height;not null" json:"bboxHeight"`
}

func (PersonFaceSample) TableName() string {
    return "person_face_samples"
}
```

---

## 6. 后端服务与领域逻辑设计

### 6.1 业务错误码定义 (`errno.go`)

在 `argus/internal/pkg/errno/errno.go` 中新增人脸专属错误码（1400 序列）：

```go
const (
    CodePersonNotFound          = 1401 // 人员不存在
    CodeFaceNoFaceDetected      = 1402 // 未检测到人脸
    CodeFaceMultipleFaces       = 1403 // 检测到多张人脸，请上传单人人脸照片
    CodeFaceQualityTooLow       = 1404 // 人脸质量过低或尺寸过小
    CodeFaceSampleLimitExceeded = 1405 // 每个人员最多注册 10 张人脸样本
    CodeFaceDuplicateImage      = 1406 // 人脸图片已存在，请勿重复注册
    CodeFaceSampleNotFound      = 1407 // 人脸样本不存在
    CodeFaceExtractionFailed    = 1408 // 人脸特征提取失败
    CodeFaceAlgoNotInstalled    = 1409 // 人脸识别算法包未安装或未激活
)
```

在 `zh-CN`、`en-US`、`zh-TW` 映射表中补充完整文案，并在 `middleware.ErrorHandler` 中建立正确的 HTTP 状态码映射。

### 6.2 注册事务与并发控制

注册流程与异常补偿机制：
1. **人员存在性检查**：查询 `persons` 确认 `person_id` 存在且未软删；
2. **图片前置校验**：校验文件格式（JPG/PNG/WebP）、文件大小（<= 10MB）、计算 SHA-256 哈希；
3. **精准重复检查**：检查 `person_face_samples` 中是否存在未软删的相同 `file_sha256` 记录，若存在返回 `CodeFaceDuplicateImage`；
4. **样本容量检查**：统计该人员未软删样本数，若 `>= 10` 返回 `CodeFaceSampleLimitExceeded`；
5. **Engine IPC 特征提取**：通过 gRPC 调用 `PersonService.ExtractFaceFeature`，将 Engine 返回的错误码精准映射为 Go 业务错误码；
6. **特征向量合规校验**：
   - 维度与字节长度必须精确为 512 个 float32（2048 字节）；
   - 浮点数不得为 NaN 或 Inf；
   - 范数在 `[0.98, 1.02]` 之间；
7. **生成唯一 SampleID 与写入存储**：
   - 生成 ULID/UUID `sampleId`；
   - 上传原图到 `person-faces/{personId}/{sampleId}_orig{ext}`；
   - 上传 112x112 特写图到 `person-faces/{personId}/{sampleId}_aligned.jpg`；
8. **持久化入库与并发安全保障**：
   - 在数据库事务中进行带有锁（或条件检查）的插入；
   - 如果发生并发插入导致突破 10 个样本上限或触发 SHA-256 唯一约束冲突，事务回滚；
   - 若入库失败，调用 `storage.Delete` 清理刚才上传的两张图片文件，防止产生孤儿存储。

### 6.3 级联删除语义

- 当删除单个人脸样本时：
  - 软删除 `person_face_samples` 记录；
  - 同步或异步清理 storage 中的原图和标准化特写图。
- 当删除人员（单人删除、批量删除、开放接口同步删除）时：
  - 事务内软删除 `persons` 记录及该人员关联的所有 `person_face_samples`；
  - 收集所有被删除样本的图片 key，成对清理 storage 文件。

---

## 7. 接口与鉴权设计

### 7.1 REST 接口列表

| 方法 | 路径 | 权限码 | 说明 |
| :--- | :--- | :--- | :--- |
| `GET` | `/api/person/page` | `resource:person` | 人员分页列表（扩展返回 `faceCount` 字段） |
| `GET` | `/api/person/:personId/faces` | `resource:person:face:manage` | 查询指定人员的所有人脸样本摘要列表 |
| `POST` | `/api/person/:personId/faces` | `resource:person:face:manage` | 为指定人员上传注册人脸样本（multipart/form-data） |
| `DELETE` | `/api/person/:personId/faces/:faceId` | `resource:person:face:manage` | 删除指定人员的单张人脸样本 |
| `GET` | `/api/person/:personId/faces/:faceId/image` | `resource:person:face:manage` | 读取受保护的原图图片文件流（inline 响应） |
| `GET` | `/api/person/:personId/faces/:faceId/aligned-image` | `resource:person:face:manage` | 读取受保护的 112x112 特写图片文件流（inline 响应） |

---

## 8. 前端 UI 与交互设计

### 8.1 人员管理主列表 (`views/resource/person/index.vue`)
- 在表格中增加“人脸样本数”列（例如：`2 / 10`），并带有彩色 Tag 或徽标；
- 操作列增加“人脸管理”按钮（带图标 `ant-design:smile-outlined` 或 `ant-design:camera-outlined`）；
- 点击打开“人脸样本管理”抽屉/弹窗。

### 8.2 人脸样本管理弹窗 (`FaceManagementModal.vue`)
- **顶部操作区**：
  - 展示当前人员标识与姓名，样本容量进度条（如 `已注册 2/10 张`）；
  - “上传人脸照片”按钮：支持点击选择或拖拽 `.jpg`, `.jpeg`, `.png`, `.webp` 文件，限制最大 10MB；
- **样本网格展示区**：
  - 卡片网格展示每张人脸样本：
    - 112x112 特写缩略图（通过带 Bearer Token 的受保护 API 流加载或 Object URL 渲染）；
    - 质量评分标签（绿色优秀 `>=70`、黄色良好 `50~69`、蓝色及格 `35~49`）；
    - 算法版本与注册时间；
    - 卡片快捷操作：查看原图（带人脸框高亮标注预览）、查看高清特写、删除样本（带二次确认 Popconfirm）；
- **图片预览组件**：
  - 使用 Ant Design Vue `Image.PreviewGroup` 或自定义弹窗，支持缩放、原图/特写对比。

### 8.3 国际化支持
在 `zh-CN/resource.json`、`en-US/resource.json`、`zh-TW/resource.json` 中补齐所有中英繁文本定义。

---

## 9. 测试与质量保证

1. **Go 单元测试与集成测试**：
   - `person_face_sample_test.go`：模型与软删除、唯一约束验证；
   - `storage_test.go`：`Get` 与 `Delete` 接口及 local / minio 实现测试；
   - `person_service_test.go`：图片格式校验、SHA-256 重复、10 张上限、Engine 异常映射、存储补偿回滚、级联删除测试；
   - `person_api_test.go`：接口路由、参数绑定、权限控制与图片流式读取响应测试；
2. **C++ 引擎与算法包测试**：
   - `person_service_test.cpp`：`ExtractFaceFeature` 请求校验、单脸/多脸/无人脸判定测试；
   - 算法包特征提取与对齐准确性单测；
3. **前端检查**：
   - `pnpm check`（TS 类型检查、oxlint、cspell 拼写检查）。
