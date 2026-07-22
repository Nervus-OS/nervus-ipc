plugins {
    // Java 工具链自动 provision。
    //
    // 没有它，libs.versions.toml 里的 jvmTarget 就退化成「构建机上碰巧装了
    // 哪个 JDK」——本机只有 JDK 21 时 jvmToolchain(17) 会直接失败：
    //   Cannot find a Java installation ... matching {languageVersion=17}
    //   Toolchain download repositories have not been configured.
    //
    // 加上之后，Gradle 按需下载并缓存指定版本的 JDK，构建结果不再取决于
    // 机器的环境差异。对要长期分发的协议产物，这个可复现性是必要的。
    id("org.gradle.toolchains.foojay-resolver-convention") version "1.0.0"
}

rootProject.name = "nervus-ipc-jvm"

// 依赖仓库集中在这里声明，而不是散落到各个 build.gradle.kts。
//
// FAIL_ON_PROJECT_REPOS 让任何子项目私自添加 repositories 直接构建失败。
// 对进入 TCB 的依赖，「代码从哪个仓库来」必须是一处可评审的事实，
// 不能由某个子模块悄悄多加一个源。
dependencyResolutionManagement {
    repositoriesMode = RepositoriesMode.FAIL_ON_PROJECT_REPOS
    repositories {
        mavenCentral()
    }
}

// protocol —— buf generate 产出的 Java + Kotlin 类型（提交进仓库，见根 README）
include("protocol")

// sdk —— 手写的 Kotlin Client / ServiceHost，同时提供 Java API（§10.10）。
// 待落地，届时取消注释。
// include("sdk")
