diff --git a/chrome/browser/metrics/chrome_metrics_service_client.cc b/chrome/browser/metrics/chrome_metrics_service_client.cc
index 1da110a55a..05feb13eb5 100644
--- a/chrome/browser/metrics/chrome_metrics_service_client.cc
+++ b/chrome/browser/metrics/chrome_metrics_service_client.cc
@@ -75,6 +75,7 @@
 #include "components/country_codes/country_codes.h"
 #include "components/crash/core/common/crash_keys.h"
 #include "components/history/core/browser/history_service.h"
+#include "chrome/browser/browseros/metrics/browseros_metrics.h"
 #include "components/metrics/call_stacks/call_stack_profile_metrics_provider.h"
 #include "components/metrics/component_metrics_provider.h"
 #include "components/metrics/content/content_stability_metrics_provider.h"
@@ -1098,6 +1099,7 @@ void ChromeMetricsServiceClient::RegisterUKMProviders() {
 }
 
 void ChromeMetricsServiceClient::NotifyApplicationNotIdle() {
+  browseros_metrics::BrowserOSMetrics::Log("alive", 0.01);
   metrics_service_->OnApplicationNotIdle();
 }
 
