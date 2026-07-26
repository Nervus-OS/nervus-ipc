import org.gradle.api.tasks.compile.JavaCompile

plugins {
    alias(libs.plugins.kotlin.jvm)
    `java-library`
}

kotlin {
    jvmToolchain(libs.versions.jvmTarget.get().toInt())
}

// 生成代码保留 proto 的 UTF-8 注释。javac 在 Windows 上默认读取系统代码页
// （常见为 GBK），必须显式固定，否则同一提交只会在 Linux 编译成功。
tasks.withType<JavaCompile>().configureEach {
    options.encoding = "UTF-8"
}

// 刻意【不】使用 protobuf-gradle-plugin。
//
// 那个插件会在构建时调用 protoc 生成代码，等于要求每台构建机都装好 protoc 且
// 版本一致——和 Go 侧拒绝「构建时生成」是同一个理由（见根 README）。这里的
// src/main/java 由 `buf generate` 写入并提交进仓库，Gradle 只负责编译已存在
// 的源码，落在它的默认约定目录上，不需要额外配置。
//
// Kotlin 插件仍然保留：生成物虽然只剩 Java，但本模块的 golden vectors 测试
// （src/test/kotlin）是 Kotlin 写的，去掉插件就编译不了测试。
dependencies {
    // 用 api 而不是 implementation：生成的消息类型会出现在本模块的公开签名里
    // （sdk 和 App 直接持有 Envelope、Request 这些类型），下游必须能传递地
    // 看到 protobuf 运行时，否则一编译就是 "cannot access GeneratedMessage"。
    api(libs.protobuf.java)
    // 【不再依赖 protobuf-kotlin】：它是 DSL builder 的运行时，而 buf.gen.yaml
    // 已停止生成 Kotlin DSL（全仓库零处使用，测试也一律走 Java .newBuilder()）。

    // golden vectors 测试（Go↔JVM 逐字节一致，NRCP §22.6）。仅测试期依赖。
    testImplementation(platform(libs.junit.bom))
    testImplementation(libs.junit.jupiter)
    testRuntimeOnly(libs.junit.platform.launcher)
}

// golden vectors 用 JUnit 5 平台运行。
tasks.test {
    useJUnitPlatform()
}
