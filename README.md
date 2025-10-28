# DuckEx-Server

## 项目介绍
DuckEx-Server 是一个使用 Go 语言编写的简单游戏物品分享服务器，名称来源于可爱的鸭子。该服务器允许玩家分享游戏物品并生成取件码，其他玩家可以通过取件码领取分享的物品。

## 功能特性
- **物品分享**：玩家可以分享物品并获得一个6位数的取件码
- **物品领取**：其他玩家可以通过取件码领取物品
- **自动过期**：分享的物品24小时后自动过期
- **健康检查**：提供API健康状态检查端点
- **CORS支持**：允许跨域请求，便于前端集成

## 技术栈
- **语言**：Go 1.21
- **Web框架**：Gin
- **存储**：内存存储（可扩展为持久化存储）

## 项目结构
```
├── cmd/
│   └── api/              # 应用程序入口
│       └── main.go       # 主程序
├── internal/
│   ├── handlers/         # HTTP处理器
│   │   └── item_handler.go
│   ├── models/           # 数据模型
│   │   └── item.go
│   └── utils/            # 工具函数
│       └── pickup_code.go
├── go.mod                # Go模块文件
├── README.md             # 项目说明
└── .gitignore
```

## 快速开始

### 安装依赖
```bash
go mod tidy
```

### 运行服务器
```bash
go run cmd/api/main.go
```

服务器将在 http://localhost:8080 启动。

## API 文档

### 健康检查
- **URL**: `/health`
- **Method**: `GET`
- **Response**:
  ```json
  {
    "status": "ok",
    "message": "DuckEx Server is quacking!",
    "timestamp": "2023-10-28T13:33:45Z"
  }
  ```

### 分享物品
- **URL**: `/api/v1/items/share`
- **Method**: `POST`
- **Request Body**:
  ```json
  {
    "name": "物品名称",
    "description": "物品描述",
    "type_id": 123,
    "num": 1,
    "durability": 95.5,
    "sharer_id": "分享者ID"
  }
  ```
- **Response**:
  ```json
  {
    "message": "Item shared successfully! Quack!",
    "pickup_code": "123456",
    "expires_at": "2023-10-29T13:33:45Z"
  }
  ```

### 领取物品
- **URL**: `/api/v1/items/claim`
- **Method**: `POST`
- **Request Body**:
  ```json
  {
    "pickup_code": "123456",
    "claimer_id": "领取者ID"
  }
  ```
- **Response**:
  ```json
  {
    "message": "Item claimed successfully! Quack!",
    "item": {
      "id": "物品ID",
      "name": "物品名称",
      "description": "物品描述",
      "type_id": 123,
      "num": 1,
      "durability": 95.5,
      "sharer_id": "分享者ID",
      "pickup_code": "123456",
      "created_at": "2023-10-28T13:33:45Z",
      "expires_at": "2023-10-29T13:33:45Z",
      "is_claimed": true,
      "claimer_id": "领取者ID"
    }
  }
  ```

## 错误处理
所有API响应都包含适当的HTTP状态码：
- `400 Bad Request`: 请求格式错误
- `404 Not Found`: 未找到物品
- `409 Conflict`: 物品已被领取
- `410 Gone`: 物品已过期
- `500 Internal Server Error`: 服务器内部错误

## 扩展建议
1. 添加持久化存储（如MySQL、PostgreSQL）
2. 实现用户认证系统
3. 添加物品类型和属性支持
4. 实现更复杂的权限控制
5. 添加物品图片上传功能

## 许可证
MIT License
