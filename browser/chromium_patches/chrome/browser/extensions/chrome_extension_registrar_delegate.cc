diff --git a/chrome/browser/extensions/chrome_extension_registrar_delegate.cc b/chrome/browser/extensions/chrome_extension_registrar_delegate.cc
index adfb4e4d49..6b4a2b5a6b 100644
--- a/chrome/browser/extensions/chrome_extension_registrar_delegate.cc
+++ b/chrome/browser/extensions/chrome_extension_registrar_delegate.cc
@@ -12,6 +12,7 @@
 #include "base/metrics/histogram_functions.h"
 #include "base/metrics/histogram_macros.h"
 #include "base/notimplemented.h"
+#include "chrome/browser/browseros/core/browseros_constants.h"
 #include "chrome/browser/extensions/component_loader.h"
 #include "chrome/browser/extensions/corrupted_extension_reinstaller.h"
 #include "chrome/browser/extensions/data_deleter.h"
@@ -26,9 +27,14 @@
 #include "chrome/browser/extensions/profile_util.h"
 #include "chrome/browser/extensions/updater/extension_updater.h"
 #include "chrome/browser/profiles/profile.h"
+#include "chrome/browser/ui/browser.h"
+#include "chrome/browser/ui/browser_list_enumerator.h"
+#include "chrome/browser/ui/tabs/tab_strip_model.h"
 #include "chrome/browser/ui/webui/favicon_source.h"
 #include "chrome/common/webui_url_constants.h"
 #include "components/favicon_base/favicon_url_parser.h"
+#include "content/public/browser/navigation_controller.h"
+#include "content/public/browser/web_contents.h"
 #include "extensions/browser/delayed_install_manager.h"
 #include "extensions/browser/disable_reason.h"
 #include "extensions/browser/extension_assets_manager.h"
@@ -50,6 +56,7 @@
 #include "extensions/common/manifest_handlers/incognito_info.h"
 #include "extensions/common/manifest_handlers/shared_module_info.h"
 #include "extensions/common/mojom/manifest.mojom-shared.h"
+#include "extensions/common/constants.h"
 #include "extensions/common/permissions/permission_message_provider.h"
 #include "extensions/common/permissions/permission_set.h"
 #include "extensions/common/permissions/permissions_data.h"
@@ -89,6 +96,46 @@ bool SkipDeleteExtensionDir(const Extension& extension,
          extension_dir_not_direct_subdir_of_unpacked_extensions_install_dir;
 }

+bool IsWuuWorkbenchURL(const GURL& url) {
+  if (url.SchemeIs(content::kChromeUIScheme)) {
+    return browseros::IsWuuBrowserProductHost(url.host());
+  }
+
+  return url.SchemeIs(extensions::kExtensionScheme) &&
+         url.host() == browseros::kAgentExtensionId &&
+         url.path() == "/app.html";
+}
+
+void ReloadPendingWuuWorkbenchTabs(Profile* profile) {
+  BrowserListEnumerator browsers;
+  while (!browsers.empty()) {
+    Browser* browser = browsers.Next();
+    if (browser->profile() != profile || browser->is_delete_scheduled()) {
+      continue;
+    }
+
+    TabStripModel* tab_strip_model = browser->tab_strip_model();
+    for (int index = 0; index < tab_strip_model->count(); ++index) {
+      content::WebContents* web_contents =
+          tab_strip_model->GetWebContentsAt(index);
+      if (!web_contents) {
+        continue;
+      }
+
+      if (!IsWuuWorkbenchURL(web_contents->GetVisibleURL()) &&
+          !IsWuuWorkbenchURL(web_contents->GetLastCommittedURL())) {
+        continue;
+      }
+
+      LOG(INFO) << "browseros: Reloading pending Wuu workbench tab after "
+                   "agent extension load";
+      web_contents->GetController().LoadURL(
+          GURL(browseros::kWuuBrowserURL), content::Referrer(),
+          ui::PAGE_TRANSITION_AUTO_TOPLEVEL, std::string());
+    }
+  }
+}
+
 }  // namespace

 ChromeExtensionRegistrarDelegate::ChromeExtensionRegistrarDelegate(
@@ -178,6 +225,10 @@ void ChromeExtensionRegistrarDelegate::PostActivateExtension(
     NOTIMPLEMENTED() << "Themes not yet supported on desktop Android.";
 #endif
   }
+
+  if (extension->id() == browseros::kAgentExtensionId) {
+    ReloadPendingWuuWorkbenchTabs(profile_);
+  }
 }

 void ChromeExtensionRegistrarDelegate::PostDeactivateExtension(
@@ -256,7 +307,17 @@ void ChromeExtensionRegistrarDelegate::PostUninstallExtension(
     }
   }

-  DataDeleter::StartDeleting(profile_, extension.get(), subtask_done_callback);
+  // Preserve chrome.storage.local data for BrowserOS extensions. These may be
+  // transiently uninstalled during update cycles (e.g., when both bundled CRX
+  // and remote config fail on startup). User configuration must survive.
+  if (browseros::IsBrowserOSExtension(extension->id())) {
+    LOG(INFO) << "browseros: Preserving storage for extension "
+              << extension->id();
+    subtask_done_callback.Run();
+  } else {
+    DataDeleter::StartDeleting(profile_, extension.get(),
+                               subtask_done_callback);
+  }
 }

 void ChromeExtensionRegistrarDelegate::DoLoadExtensionForReload(
@@ -322,6 +383,13 @@ bool ChromeExtensionRegistrarDelegate::CanDisableExtension(
     return true;
   }

+  // - BrowserOS extensions cannot be disabled by users
+  if (browseros::IsBrowserOSExtension(extension->id())) {
+    LOG(INFO) << "browseros: Extension " << extension->id()
+              << " cannot be disabled (BrowserOS extension)";
+    return false;
+  }
+
   // - Shared modules are just resources used by other extensions, and are not
   //   user-controlled.
   if (SharedModuleInfo::IsSharedModule(extension)) {
