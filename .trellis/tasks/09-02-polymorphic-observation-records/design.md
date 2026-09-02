# 多态通用识别记录设计方案 (Technical Design)

## 1. 架构总览与分层关系

```text
┌─────────────────────────────────────────────────────────────────────────┐
│ 前端 UI (Vue3 + Vben Admin 5.7)                                          │
│   /record/observation (方案 A 胶囊: [ 全部 ] [ 👤 人员 ] [ 🚗 车辆 ])        │
│   ├── Segmented 目标类型自适应切换                                        │
│   ├── 多态表格: 人脸/车牌特写自适应 + 姓名/车牌号 Tag 自适应                │
│   └── ObservationDetailDrawer: 人脸 Top-5 候选 vs 车牌/车辆多维属性抽屉   │
└────────────────────────────────────┬────────────────────────────────────┘
                                     │ HTTP REST
                                     ▼
┌─────────────────────────────────────────────────────────────────────────┐
│ Go 后端 API & 聚合服务 (ObservationService & Handler)                     │
│   GET /api/record/observations?targetType=all|person|vehicle&keyword=...│
│   GET /api/record/observations/:id?targetType=person|vehicle            │
│   GET /api/record/observations/:id/image?targetType=...&kind=...        │
│                                                                         │
│   ├── targetType == "person":  下推 FaceObservationRepository           │
│   ├── targetType == "vehicle": 下推 PlateObservationRepository          │
│   └── targetType == "all":     联合分页归并 (Time Descending Merge)     │
└──────────────────┬───────────────────────────────────┬──────────────────┘
                   │                                   │
                   ▼                                   ▼
┌────────────────────────────────────┐ ┌──────────────────────────────────┐
│ face_observations (人员底库识别)    │ │ plate_observations (车牌识别)    │
│  - 静态人员库 1:N 比对凭证          │ │  - 车牌通行与 OCR 属性凭证       │
│  - Top-5 候选底库快照               │ │  - 牌照颜色 / 车型 / 置信度      │
└────────────────────────────────────┘ └──────────────────────────────────┘
```

---

## 2. 接口与数据契约设计

### 2.1 统一列表查询入参 (`ObservationQuery`)
```go
type ObservationQuery struct {
    Page       int        `form:"page"`
    PageSize   int        `form:"pageSize"`
    TargetType string     `form:"targetType"` // "all" | "person" | "vehicle"
    CameraID   string     `form:"cameraId"`
    StartTime  *time.Time `form:"startTime"`
    EndTime    *time.Time `form:"endTime"`
    Keyword    string     `form:"keyword"` // 模糊匹配人员姓名、工号、车牌号
    TrackID    int64      `form:"trackId"`
}
```

### 2.2 统一响应 DTO (`ObservationItem`)
```go
type ObservationItem struct {
    ID               uint64         `json:"id"`
    EventID          string         `json:"eventId"`
    TargetType       string         `json:"targetType"` // "person" | "vehicle"
    CameraID         string         `json:"cameraId"`
    CameraName       string         `json:"cameraName"`
    AlgorithmID      string         `json:"algorithmId"`
    AlgorithmVersion string         `json:"algorithmVersion"`
    TrackID          int64          `json:"trackId"`
    ObservedAt       time.Time      `json:"observedAt"`
    TimeSynced       bool           `json:"timeSynced"`
    
    // 主体信息 (Subject)
    SubjectID        string         `json:"subjectId"`   // 人员工号 / 车牌号
    SubjectName      string         `json:"subjectName"` // 人员姓名 / 车牌号
    Confidence       float32        `json:"confidence"`
    Similarity       float32        `json:"similarity"`  // 人脸相似度 / 车牌置信度
    
    // 视觉图片
    ImageURL         string         `json:"imageUrl,omitempty"`
    CropImageURL     string         `json:"cropImageUrl,omitempty"`
    BBox             []float32      `json:"bbox"`
    SubBBox          []float32      `json:"subBbox,omitempty"`
    
    // 多态扩展属性
    Attributes       map[string]any `json:"attributes"`
}
```

---

## 3. 联合分页归并算法 (Time-Descending Merge)

当 `targetType == "all"` 时：
1. 分别从 `faceRepo.CountTotal(...)` 和 `plateRepo.CountTotal(...)` 获取两表总数并求和为 `total`；
2. 为支持任意 offset 的高效分页，调用各仓储以 `Limit(offset + pageSize)` 查询按时间倒序的前 N 条数据；
3. 使用双指针（Two-Pointer / Priority Queue）对两组切片按 `observed_at DESC, id DESC` 归并排序；
4. 截取 `[offset : offset + pageSize]` 区间转换为统一的 `ObservationItem` 并返回。

---

## 4. 路由平滑过渡与 RBAC 兼容设计

1. **数据库菜单种子更新**：
   - 菜单名由 `RecordFace` 升级为 `RecordObservation`；
   - 路径为 `/record/observation`，组件为 `/record/observation/index`；
   - 标题为 `routes.record.observation`；
   - 权限码注册为 `record:observation`（同时保持 `record:face` 别名兼容）。
2. **前端路由配置**：
   - 在前端路由或导航守卫中，保留 `/record/face` 路由作为对 `/record/observation` 的重定向，确保用户历史收藏夹与直接输入 URL 访问均无缝跳转。
