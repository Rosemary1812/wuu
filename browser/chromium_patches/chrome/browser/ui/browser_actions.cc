diff --git a/chrome/browser/ui/browser_actions.cc b/chrome/browser/ui/browser_actions.cc
index 19d0181ac8285..f0ed70a42d5b1 100644
--- a/chrome/browser/ui/browser_actions.cc
+++ b/chrome/browser/ui/browser_actions.cc
@@ -273,6 +273,36 @@ void BrowserActions::InitializeBrowserActions() {
             .Build());
   }
 
+  // Add third-party LLM panel if feature is explicitly enabled.
+  if (base::FeatureList::IsEnabled(features::kThirdPartyLlmPanel)) {
+    root_action_item_->AddChild(
+        SidePanelAction(SidePanelEntryId::kThirdPartyLlm,
+                        IDS_THIRD_PARTY_LLM_TITLE,
+                        IDS_THIRD_PARTY_LLM_TITLE,
+                        vector_icons::kChatOrangeIcon,
+                        kActionSidePanelShowThirdPartyLlm, bwi, true)
+            .Build());
+  }
+
+  // Add Clash of GPTs action if feature is explicitly enabled.
+  if (base::FeatureList::IsEnabled(features::kClashOfGpts)) {
+    root_action_item_->AddChild(
+        ChromeMenuAction(
+            base::BindRepeating(
+                [](BrowserWindowInterface* bwi, actions::ActionItem* item,
+                   actions::ActionInvocationContext context) {
+                  if (auto* browser_view =
+                          BrowserView::GetBrowserViewForBrowser(bwi)) {
+                    chrome::ExecuteCommand(browser_view->browser(),
+                                           IDC_OPEN_CLASH_OF_GPTS);
+                  }
+                },
+                bwi),
+            kActionSidePanelShowClashOfGpts,
+            IDS_CLASH_OF_GPTS_TITLE,
+            IDS_CLASH_OF_GPTS_TOOLTIP,
+            vector_icons::kClashOfGptsIcon)
+            .Build());
+  }
+
   if (HistorySidePanelCoordinator::IsSupported()) {
     root_action_item_->AddChild(
         SidePanelAction(SidePanelEntryId::kHistory, IDS_HISTORY_TITLE,
