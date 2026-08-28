// Package web 提供 Vite 构建产物的 embed.FS。
// 构建前需先在 web/ 下 `npm run build` 生成 dist/；Makefile 已前置该步骤。
package web

import "embed"

// DistFS 是 web/dist 的嵌入文件系统，由 internal/server 挂载到 / 与 /admin。
//
//go:embed dist
var DistFS embed.FS
