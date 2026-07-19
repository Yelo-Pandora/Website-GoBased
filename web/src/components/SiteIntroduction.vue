<script setup>
import {BookOpen, Boxes, FlaskConical, Route, ServerCog} from '@lucide/vue';

const learningSteps = [
  {title: '单机架构', copy: '先理解入口、应用和数据集中部署时的职责与限制。'},
  {title: '应用与数据分离', copy: '再观察应用和数据库拆开后新增的网络边界。'},
  {title: '应用集群', copy: '通过扩容、权重和自适应控制学习负载均衡。'},
  {title: '多级缓存', copy: '比较进程内 L1、Redis 与 MySQL 的读取路径，并理解常见缓存故障。'},
];
</script>

<template>
  <main class="site-introduction">
    <section class="introduction-hero">
      <span class="introduction-hero__mark"><ServerCog :size="30" /></span>
      <p class="eyebrow">Welcome</p>
      <h1>从理论到真实资源，理解后端架构为什么演进</h1>
      <p>
        这是一个面向互联网后端架构学习的交互平台。
        你可以先阅读公开理论，再登录创建隔离实验，观察 Go 服务、Nginx、MySQL、Redis
        和容器资源如何共同处理请求。
      </p>
    </section>

    <section class="introduction-principles" aria-label="平台使用方式">
      <article>
        <BookOpen :size="22" />
        <h2>理论对游客开放</h2>
        <p>无需登录即可浏览课程正文，先建立概念，再决定是否进入实验。</p>
      </article>
      <article>
        <FlaskConical :size="22" />
        <h2>实验需要登录</h2>
        <p>登录后平台会创建真实且隔离的实验资源，并记录操作与学习进度。</p>
      </article>
      <article>
        <Boxes :size="22" />
        <h2>结果来自真实后端</h2>
        <p>拓扑、流量、缓存和权重都来自实际服务状态，而不是前端静态演示。</p>
      </article>
    </section>

    <section class="learning-route" aria-labelledby="learning-route-title">
      <div class="section-heading">
        <Route :size="19" />
        <h2 id="learning-route-title">推荐学习路线</h2>
      </div>
      <ol>
        <li v-for="(step, index) in learningSteps" :key="step.title">
          <span>{{ index + 1 }}</span>
          <div><strong>{{ step.title }}</strong><p>{{ step.copy }}</p></div>
        </li>
      </ol>
    </section>
  </main>
</template>
