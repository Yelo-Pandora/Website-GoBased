<script setup>
import {
  AlertTriangle,
  Cpu,
  Database,
  Globe2,
  HardDrive,
  MemoryStick,
  Server,
  UserRound,
} from '@lucide/vue';
</script>

<template>
  <section class="standalone-diagram" aria-labelledby="standalone-diagram-title">
    <header>
      <p class="eyebrow">Architecture Map</p>
      <h2 id="standalone-diagram-title">单机架构请求与资源边界</h2>
      <p>入口、应用与数据库运行在同一台主机中，共享资源，也共享故障范围。</p>
    </header>

    <div class="standalone-diagram__flow" aria-label="用户请求进入单机主机后依次经过 Nginx、Go 应用和 MySQL">
      <article class="architecture-node architecture-node--user">
        <UserRound :size="24" />
        <strong>用户</strong>
        <small>商品请求</small>
      </article>
      <span class="architecture-arrow" aria-hidden="true">→</span>
      <div class="single-host-boundary">
        <div class="single-host-boundary__title">
          <Server :size="19" />
          <strong>单机主机</strong>
          <small>同一故障边界</small>
        </div>
        <div class="single-host-boundary__services">
          <article class="architecture-node">
            <Globe2 :size="23" />
            <strong>Nginx / Web</strong>
            <small>统一入口</small>
          </article>
          <span class="architecture-arrow" aria-hidden="true">→</span>
          <article class="architecture-node">
            <Server :size="23" />
            <strong>Go 应用</strong>
            <small>业务处理</small>
          </article>
          <span class="architecture-arrow" aria-hidden="true">→</span>
          <article class="architecture-node">
            <Database :size="23" />
            <strong>MySQL</strong>
            <small>业务数据</small>
          </article>
        </div>
        <div class="shared-resources" aria-label="共享主机资源">
          <span><Cpu :size="17" />共享 CPU</span>
          <span><MemoryStick :size="17" />共享内存</span>
          <span><HardDrive :size="17" />共享磁盘</span>
        </div>
      </div>
    </div>

    <div class="standalone-diagram__warning">
      <AlertTriangle :size="20" />
      <p><strong>单点故障：</strong>主机停机时，入口、应用和数据库会同时不可用。</p>
    </div>
  </section>
</template>
