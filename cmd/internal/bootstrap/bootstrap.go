// Package bootstrap 保留进程装配入口；具体参考宿主与规则位于 development adapter。
package bootstrap

import "github.com/ruipengliu/lerna/adapters/development"

type Config = development.Config
type ModelConfig = development.ModelConfig
type BuiltinGovernanceConfig = development.BuiltinGovernanceConfig
type App = development.App

var LoadConfig = development.LoadConfig
var SaveConfig = development.SaveConfig
var DevelopmentConfig = development.DevelopmentConfig
var InitializeConfig = development.InitializeConfig
var DSN = development.DSN
var OpenStore = development.OpenStore
var OpenApp = development.OpenApp
var OpenAppForRole = development.OpenAppForRole
var RequiredContentPurposes = development.RequiredContentPurposes
