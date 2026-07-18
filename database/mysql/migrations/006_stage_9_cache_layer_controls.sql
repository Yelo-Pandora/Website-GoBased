USE platform;

UPDATE courses
SET status = 'theory',
    summary = '讲解缓存穿透、击穿、雪崩、Redis 故障与常见保护思路。'
WHERE slug = 'cache-failures';
