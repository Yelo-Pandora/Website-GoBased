import assert from 'node:assert/strict';
import test from 'node:test';

import {renderCourseMarkdown} from './render-course-markdown.js';

test('renders course markdown features', () => {
  const result = renderCourseMarkdown(`# 标题

| 层级 | 资源 |
| --- | --- |
| L1 | 应用进程 |

\`\`\`go
fmt.Println("ok")
\`\`\`

[参考](https://example.com)
`);

  assert.match(result, /<h1>标题<\/h1>/);
  assert.match(result, /<table>/);
  assert.match(result, /<code class="language-go">/);
  assert.match(result, /target="_blank"/);
  assert.match(result, /rel="noopener noreferrer"/);
});

test('does not render raw HTML from course content', () => {
  const result = renderCourseMarkdown('<script>alert("unsafe")</script>');

  assert.doesNotMatch(result, /<script>/);
  assert.match(result, /&lt;script&gt;/);
});
