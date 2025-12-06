package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"sync"
	"time"

	"golang.org/x/net/proxy"
)

type Backend struct {
	URL       string
	Healthy   bool
	LastCheck time.Time
	mu        sync.RWMutex
}

type SessionMapping struct {
	Backend   string
	Timestamp time.Time
}

type LoadBalancer struct {
	backends        []*Backend
	sessionMap      map[string]*SessionMapping // key: IP+CPN
	sessionMu       sync.RWMutex
	torProxyURL     string
	currentIndex    int
	indexMu         sync.Mutex
	useProxyMode    bool
}

var (
	backendURLs = []string{
		"https://inv-eu2.nadeko.net",
		"https://inv-eu3.nadeko.net",
		"https://inv-eu4.nadeko.net",
		"https://inv-eu5.nadeko.net",
		"https://inv-us1.nadeko.net",
		"https://inv-us2.nadeko.net",
	}
)

func NewLoadBalancer(torProxy string) *LoadBalancer {
	// Check environment variable for proxy mode (default to true for backward compatibility)
	useProxyMode := true
	if proxyMode := os.Getenv("USE_PROXY_MODE"); proxyMode != "" {
		useProxyMode = proxyMode == "true" || proxyMode == "1" || proxyMode == "yes"
	}

	lb := &LoadBalancer{
		backends:     make([]*Backend, 0),
		sessionMap:   make(map[string]*SessionMapping),
		torProxyURL:  torProxy,
		useProxyMode: useProxyMode,
	}

	for _, backendURL := range backendURLs {
		lb.backends = append(lb.backends, &Backend{
			URL:     backendURL,
			Healthy: false,
		})
	}

	return lb
}

func (lb *LoadBalancer) checkHealth(backend *Backend) {
	backend.mu.Lock()
	defer backend.mu.Unlock()

	healthURL := backend.URL + "/health"
	
	// ساخت HTTP client با Tor proxy
	dialer, err := proxy.SOCKS5("tcp", "tor:9050", nil, proxy.Direct)
	if err != nil {
		log.Printf("خطا در اتصال به Tor: %v", err)
		backend.Healthy = false
		return
	}

	transport := &http.Transport{
		Dial: dialer.Dial,
	}
	
	client := &http.Client{
		Transport: transport,
		Timeout:   10 * time.Second,
	}

	resp, err := client.Get(healthURL)
	if err != nil {
		log.Printf("Backend %s غیرفعال شد: %v", backend.URL, err)
		backend.Healthy = false
		backend.LastCheck = time.Now()
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		backend.Healthy = true
		log.Printf("Backend %s فعال است", backend.URL)
	} else {
		backend.Healthy = false
		log.Printf("Backend %s وضعیت نامعتبر دارد: %d", backend.URL, resp.StatusCode)
	}
	
	backend.LastCheck = time.Now()
}

func (lb *LoadBalancer) startHealthChecks(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	// بررسی اولیه
	lb.checkAllBackends()

	for {
		select {
		case <-ticker.C:
			lb.checkAllBackends()
		case <-ctx.Done():
			return
		}
	}
}

func (lb *LoadBalancer) checkAllBackends() {
	var wg sync.WaitGroup
	for _, backend := range lb.backends {
		wg.Add(1)
		go func(b *Backend) {
			defer wg.Done()
			lb.checkHealth(b)
		}(backend)
	}
	wg.Wait()
}

func (lb *LoadBalancer) cleanupSessions(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			lb.sessionMu.Lock()
			now := time.Now()
			for key, session := range lb.sessionMap {
				if now.Sub(session.Timestamp) > time.Hour {
					delete(lb.sessionMap, key)
				}
			}
			lb.sessionMu.Unlock()
		case <-ctx.Done():
			return
		}
	}
}

func (lb *LoadBalancer) getHealthyBackends() []*Backend {
	healthy := make([]*Backend, 0)
	for _, backend := range lb.backends {
		backend.mu.RLock()
		if backend.Healthy {
			healthy = append(healthy, backend)
		}
		backend.mu.RUnlock()
	}
	return healthy
}

func (lb *LoadBalancer) getBackendForSession(clientIP, cpn string) string {
	sessionKey := clientIP + ":" + cpn

	lb.sessionMu.RLock()
	session, exists := lb.sessionMap[sessionKey]
	lb.sessionMu.RUnlock()

	if exists && time.Since(session.Timestamp) < time.Hour {
		// بررسی اینکه backend هنوز سالم است
		for _, backend := range lb.backends {
			backend.mu.RLock()
			if backend.URL == session.Backend && backend.Healthy {
				backend.mu.RUnlock()
				// به‌روزرسانی timestamp
				lb.sessionMu.Lock()
				session.Timestamp = time.Now()
				lb.sessionMu.Unlock()
				return session.Backend
			}
			backend.mu.RUnlock()
		}
	}

	// انتخاب backend جدید
	healthyBackends := lb.getHealthyBackends()
	if len(healthyBackends) == 0 {
		return ""
	}

	lb.indexMu.Lock()
	selectedBackend := healthyBackends[lb.currentIndex%len(healthyBackends)]
	lb.currentIndex++
	lb.indexMu.Unlock()

	// ذخیره session
	lb.sessionMu.Lock()
	lb.sessionMap[sessionKey] = &SessionMapping{
		Backend:   selectedBackend.URL,
		Timestamp: time.Now(),
	}
	lb.sessionMu.Unlock()

	return selectedBackend.URL
}

func (lb *LoadBalancer) handleVideoPlayback(w http.ResponseWriter, r *http.Request) {
	cpn := r.URL.Query().Get("cpn")
	if cpn == "" {
		http.Error(w, "پارامتر cpn یافت نشد", http.StatusBadRequest)
		return
	}

	clientIP := r.RemoteAddr
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		clientIP = fwd
	}

	backendURL := lb.getBackendForSession(clientIP, cpn)
	if backendURL == "" {
		http.Error(w, "هیچ backend سالمی در دسترس نیست", http.StatusServiceUnavailable)
		return
	}

	targetURL, err := url.Parse(backendURL)
	if err != nil {
		http.Error(w, "خطا در پردازش URL", http.StatusInternalServerError)
		return
	}

	if lb.useProxyMode {
		// ساخت reverse proxy
		proxy := httputil.NewSingleHostReverseProxy(targetURL)
		
		// تنظیم path به /companion/videoplayback
		r.URL.Path = "/companion/videoplayback"
		r.URL.Host = targetURL.Host
		r.URL.Scheme = targetURL.Scheme
		r.Host = targetURL.Host

		log.Printf("پروکسی %s با cpn=%s به %s", clientIP, cpn, backendURL)
		
		proxy.ServeHTTP(w, r)
	} else {
		// تغییر path به /companion/videoplayback و ریدایرکت
		redirectURL := backendURL + "/companion/videoplayback"
		
		// اضافه کردن پارامترهای کوئری
		query := r.URL.Query()
		queryString := query.Encode()
		if queryString != "" {
			redirectURL += "?" + queryString
		}

		log.Printf("ریدایرکت %s با cpn=%s به %s", clientIP, cpn, redirectURL)
		
		http.Redirect(w, r, redirectURL, http.StatusTemporaryRedirect)
	}
}

func (lb *LoadBalancer) handleOtherRequests(w http.ResponseWriter, r *http.Request) {
	// ساخت reverse proxy برای piped-proxy
	targetURL, err := url.Parse("http://piped-proxy:8080")
	if err != nil {
		http.Error(w, "خطا در پردازش URL برای piped-proxy", http.StatusInternalServerError)
		return
	}

	// ساخت reverse proxy
	proxy := httputil.NewSingleHostReverseProxy(targetURL)
	
	// تنظیمات اصلی
	r.URL.Host = targetURL.Host
	r.URL.Scheme = targetURL.Scheme
	r.Host = targetURL.Host

	log.Printf("ارسال درخواست %s به %s", r.URL.Path, targetURL.String())
	
	proxy.ServeHTTP(w, r)
}

func (lb *LoadBalancer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/videoplayback" {
		lb.handleVideoPlayback(w, r)
	} else if r.URL.Path == "/health" {
		lb.handleHealth(w, r)
	} else {
		// تمام درخواست‌های دیگر به piped-proxy فرستاده می‌شوند
		lb.handleOtherRequests(w, r)
	}
}

func (lb *LoadBalancer) handleHealth(w http.ResponseWriter, r *http.Request) {
	healthyCount := 0
	for _, backend := range lb.backends {
		backend.mu.RLock()
		if backend.Healthy {
			healthyCount++
		}
		backend.mu.RUnlock()
	}

	if healthyCount > 0 {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "OK - %d/%d backends سالم هستند", healthyCount, len(lb.backends))
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprintf(w, "هیچ backend سالمی در دسترس نیست")
	}
}

func main() {
	lb := NewLoadBalancer("socks5://tor:9050")

	ctx := context.Background()
	
	// شروع health checks
	go lb.startHealthChecks(ctx)
	
	// شروع پاکسازی sessions
	go lb.cleanupSessions(ctx)

	// استفاده از متد ServeHTTP برای مدیریت درخواست‌ها
	log.Println("Load balancer شروع شد روی پورت 8080")
	if err := http.ListenAndServe(":8080", lb); err != nil {
		log.Fatal(err)
	}
}