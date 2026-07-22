plugins {
    alias(libs.plugins.kotlin.jvm)
    `java-library`
}

kotlin {
    jvmToolchain(libs.versions.jvmTarget.get().toInt())
}

// 刻意【不】使用 protobuf-gradle-plugin。
//
// 那个插件会在构建时调用 protoc 生成代码，等于要求每台构建机都装好 protoc 且
// 版本一致——和 Go 侧拒绝「构建时生成」是同一个理由（见根 README）。这里的
// src/main/java 与 src/main/kotlin 由 `buf generate` 写入并提交进仓库，
// Gradle 只负责编译已存在的源码，落在它的默认约定目录上，不需要额外配置。
dependencies {
    // 用 api 而不是 implementation：生成的消息类型会出现在本模块的公开签名里
    // （sdk 和 App 直接持有 Envelope、Request 这些类型），下游必须能传递地
    // 看到 protobuf 运行时，否则一编译就是 "cannot access GeneratedMessage"。
    api(libs.protobuf.java)
    api(libs.protobuf.kotlin)
}
