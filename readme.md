# Invidious Load Balancer

این برنامه یک load balancer برای سرویس‌های Invidious است که از طریق Tor برای بررسی سلامت سرورها استفاده می‌کند.

## ویژگی‌ها

- ✅ بررسی سلامت بک‌اندها هر 5 دقیقه از طریق Tor
- ✅ مسیریابی هوشمند بر اساس IP و CPN کاربر
- ✅ نگهداری session تا 1 ساعت
- ✅ پاکسازی خودکار session‌های منقضی شده
- ✅ Load balancing با الگوریتم Round Robin
- ✅ Docker و Docker Compose با Tor داخلی
- ✅ Health checks برای هر دو سرویس
- ✅ پشتیبانی از چندین سرویس: Invidious (videoplayback) و Piped (سایر درخواست‌ها)
- ✅ مسیریابی هوشمند درخواست‌ها بین سرویس‌ها

## نصب و راه‌اندازی

### پیش‌نیازها
- Docker
- Docker Compose

### ساختار فایل‌های پروژه

```
invidious-loadbalancer/
├── main.go              # کد اصلی برنامه
├── Dockerfile           # Docker image برای load balancer
├── Dockerfile.tor       # Docker image برای Tor
├── docker-compose.yml   # تنظیمات Docker Compose
├── go.mod              # وابستگی‌های Go
├── go.sum              # Checksums وابستگی‌ها
├── torrc               # پیکربندی Tor
└── README.md           # این فایل
```

### مراحل نصب

1. کلون کردن یا ساخت پروژه:
```bash
mkdir invidious-loadbalancer
cd invidious-loadbalancer
```

2. ایجاد تمام فایل‌های پروژه (از artifacts فراهم شده)

3. اجرای برنامه:
```bash
docker-compose up -d
```

4. بررسی لاگ‌ها:
```bash
docker-compose logs -f loadbalancer
docker-compose logs -f tor
```

5. بررسی وضعیت سرویس‌ها:
```bash
docker-compose ps
```

## استفاده

### Endpoint ها

- **Health Check**: `http://localhost:8081/health`
- **Video Playback**: `http://localhost:8081/videoplayback?cpn=...&[other-params]`
- **Piped Requests**: `http://localhost:8081/[other-endpoints]` (همه درخواست‌های غیر از /videoplayback به سرویس Piped Proxy فرستاده می‌شوند)

### مسیریابی درخواست‌ها

- درخواست‌هایی که به `/videoplayback` پایان می‌یابند، به بک‌اندهای Invidious مسیریابی می‌شوند
- تمام سایر درخواست‌ها به سرویس Piped Proxy ارسال می‌شوند

### مثال درخواست

```bash
curl "http://localhost:8081/videoplayback?cpn=W41TgQcaXEPhoash&itag=140&..."
```

### تست اتصال Tor

```bash
# بررسی اینکه Tor در حال اجرا است
docker-compose exec tor nc -zv localhost 9050

# یا با curl از طریق Tor
curl --socks5 localhost:9050 https://check.torproject.org
```

## معماری

برنامه شامل سه سرویس Docker است:

1. **Tor Proxy**: 
   - بر اساس Alpine Linux
   - استفاده از فایل پیکربندی torrc سفارشی
   - Health check داخلی
   - Volume برای ذخیره داده‌های Tor
   - استفاده می‌شود برای بررسی سلامت بک‌اندها

2. **Piped Proxy**: 
   - سرویس 1337kavin/piped-proxy:latest
   - مدیریت درخواست‌های غیر از /videoplayback
   - ارائه واسط Piped برای جایگزین کردن یوتیوب

3. **Load Balancer**: 
   - برنامه Go
   - اتصال به Tor برای health checks
   - مسیریابی هوشمند درخواست‌ها:
     - درخواست‌های /videoplayback را به بک‌اندهای Invidious هدایت می‌کند
     - سایر درخواست‌ها را به سرویس Piped Proxy هدایت می‌کند
   - مدیریت session و توزیع بار
   - Health check endpoint

### بک‌اندهای پیش‌فرض

- https://backend1.example.com
- https://backend2.example.com
- https://backend3.example.com
- https://backend4.example.com
- https://backend5.example.com
- https://backend6.example.com

## مدیریت Session

برنامه برای هر ترکیب (IP + CPN) یک session ایجاد می‌کند که:
- تا 1 ساعت معتبر است
- کاربر را به همان بک‌اند قبلی هدایت می‌کند
- به صورت خودکار پاکسازی می‌شود

## تنظیمات Tor

فایل `torrc` شامل تنظیمات بهینه شده برای load balancing است:
- Circuit build timeout کاهش یافته
- تعداد entry guards افزایش یافته
- زمان تغییر circuit کوتاه‌تر

برای تغییر تنظیمات، فایل `torrc` را ویرایش کنید.

## توقف و راه‌اندازی مجدد

```bash
# توقف سرویس‌ها
docker-compose down

# توقف و حذف volumes
docker-compose down -v

# راه‌اندازی مجدد
docker-compose restart

# راه‌اندازی مجدد فقط یک سرویس
docker-compose restart loadbalancer
docker-compose restart tor
```

## تنظیمات

برنامه با استفاده از متغیر محیطی قابل پیکربندی است:

### USE_PROXY_MODE
- **توضیحات**: تعیین می‌کند که درخواست‌های `/videoplayback` باید پروکسی شوند یا ریدایرکت شوند
- **مقادیر ممکن**: `true` (پیش‌فرض)، `false`، `1`، `0`، `yes`، `no`
- **پیش‌فرض**: `true` (پروکسی)
- **مثال**: `USE_PROXY_MODE=false` (در این حالت درخواست‌ها ریدایرکت می‌شوند)

### TZ
- **توضیحات**: تنظیم منطقه زمانی سرور
- **پیش‌فرض**: `Asia/Tehran`

## توسعه

برای تغییر تنظیمات:
1. فایل‌های مورد نظر را ویرایش کنید
2. برنامه را دوباره build کنید:
```bash
docker-compose up -d --build
```

## لاگ‌ها

```bash
# تمام لاگ‌ها
docker-compose logs -f

# فقط load balancer
docker-compose logs -f loadbalancer

# فقط Tor
docker-compose logs -f tor

# 100 خط آخر
docker-compose logs --tail=100 loadbalancer
```

## مشکلات رایج

### Tor متصل نمی‌شود
```bash
# بررسی وضعیت
docker-compose ps tor

# مشاهده لاگ‌ها
docker-compose logs tor

# راه‌اندازی مجدد
docker-compose restart tor
```

### هیچ بک‌اندی سالم نیست
- صبر کنید تا health check اول انجام شود (تا 5 دقیقه)
- لاگ‌ها را بررسی کنید:
```bash
docker-compose logs -f loadbalancer
```
- به صورت دستی یکی از بک‌اندها را تست کنید:
```bash
curl --socks5 localhost:9050 https://backend1.example.com/health
```

### خطای Build
```bash
# پاک کردن cache و build مجدد
docker-compose build --no-cache
docker-compose up -d
```

## مانیتورینگ

برای مانیتورینگ بهتر می‌توانید:
1. لاگ‌ها را به یک سیستم logging مرکزی ارسال کنید
2. از Prometheus برای جمع‌آوری metrics استفاده کنید
3. Grafana برای نمایش داده‌ها راه‌اندازی کنید

## امنیت

- Tor فقط برای health checks استفاده می‌شود
- ترافیک کاربران مستقیماً به بک‌اندها ارسال می‌شود
- هیچ داده کاربری در load balancer ذخیره نمی‌شود
- Session ها فقط شامل IP و CPN هستند

## لایسنس

MIT