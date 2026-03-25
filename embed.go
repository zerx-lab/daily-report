package dailyreport

import "embed"

// TemplatesFS 嵌入所有 HTML 模板文件（templates 目录）
//
//go:embed templates/*
var TemplatesFS embed.FS

// StaticFS 嵌入所有静态资源文件（static 目录）
//
//go:embed static/*
var StaticFS embed.FS
