diff --git a/chrome/browser/favicon/favicon_utils.cc b/chrome/browser/favicon/favicon_utils.cc
index c67469586899e..9e4eeae358e2e 100644
--- a/chrome/browser/favicon/favicon_utils.cc
+++ b/chrome/browser/favicon/favicon_utils.cc
@@ -6,6 +6,7 @@
 
 #include "base/hash/sha1.h"
 #include "build/build_config.h"
+#include "chrome/browser/browseros/core/browseros_constants.h"
 #include "chrome/browser/favicon/favicon_service_factory.h"
 #include "chrome/browser/profiles/profile.h"
 #include "chrome/common/url_constants.h"
@@ -184,6 +185,9 @@ bool ShouldThemifyFavicon(GURL url) {
   if (!url.SchemeIs(content::kChromeUIScheme)) {
     return false;
   }
+  if (browseros::IsWuuBrowserProductHost(url.host())) {
+    return false;
+  }
   return url.host() != chrome::kChromeUIAppLauncherPageHost &&
          url.host() != chrome::kChromeUIHelpHost &&
          url.host() != chrome::kChromeUIVersionHost &&
