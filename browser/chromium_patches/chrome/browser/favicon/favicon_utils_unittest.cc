diff --git a/chrome/browser/favicon/favicon_utils_unittest.cc b/chrome/browser/favicon/favicon_utils_unittest.cc
index 62c5677345097..808aa2d6c62d6 100644
--- a/chrome/browser/favicon/favicon_utils_unittest.cc
+++ b/chrome/browser/favicon/favicon_utils_unittest.cc
@@ -49,6 +49,11 @@ TEST(FaviconUtilsTest, ShouldThemifyFavicon) {
   entry->SetVirtualURL(themeable_virtual_url);
   // Entry should be themefied if only its virtual URL is themeable.
   EXPECT_TRUE(ShouldThemifyFaviconForEntry(entry.get()));
+
+  entry->SetVirtualURL(GURL("chrome://wuu/"));
+  entry->SetURL(unthemeable_url);
+  // Wuu is backed by an extension UI and has a full-color product favicon.
+  EXPECT_FALSE(ShouldThemifyFaviconForEntry(entry.get()));
 }
 
 class DefaultFaviconModelTest : public ::testing::Test {
