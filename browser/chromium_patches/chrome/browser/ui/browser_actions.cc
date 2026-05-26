diff --git a/chrome/browser/ui/browser_actions.cc b/chrome/browser/ui/browser_actions.cc
index 19d0181ac8285..aa91e6d2c2 100644
--- a/chrome/browser/ui/browser_actions.cc
+++ b/chrome/browser/ui/browser_actions.cc
@@ -273,6 +273,9 @@ void BrowserActions::InitializeBrowserActions() {
             .Build());
   }
 
+  // Wuu hides the legacy BrowserOS AI toolbar entries while the browser
+  // side-panel and Workbench entry points are being redesigned.
+
   if (HistorySidePanelCoordinator::IsSupported()) {
     root_action_item_->AddChild(
         SidePanelAction(SidePanelEntryId::kHistory, IDS_HISTORY_TITLE,
