diff --git a/chrome/browser/ui/startup/startup_browser_creator_impl.cc b/chrome/browser/ui/startup/startup_browser_creator_impl.cc
index 72b132d45c..d4cca8df71 100644
--- a/chrome/browser/ui/startup/startup_browser_creator_impl.cc
+++ b/chrome/browser/ui/startup/startup_browser_creator_impl.cc
@@ -18,6 +18,7 @@
 #include "base/notreached.h"
 #include "base/version.h"
 #include "build/build_config.h"
+#include "chrome/browser/browseros/core/browseros_constants.h"
 #include "chrome/browser/apps/platform_apps/install_chrome_app.h"
 #include "chrome/browser/browser_process.h"
 #include "chrome/browser/custom_handlers/protocol_handler_registry_factory.h"
@@ -626,7 +627,8 @@ StartupBrowserCreatorImpl::DetermineStartupTabs(
     // Note that URLs from preferences are explicitly meant to override showing
     // the NTP.
     if (prefs_tabs.empty()) {
-      AppendTabs(provider.GetNewTabPageTabs(*command_line_, profile_), &tabs);
+      AppendTabs(StartupTabs({StartupTab(GURL(browseros::kWuuBrowserURL))}),
+                 &tabs);
     }

     // Potentially add a tab appropriate to display the Privacy Sandbox
