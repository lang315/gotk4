#include <glib-object.h>

// goToggleNotify and goFinishRemovingToggleRef are implemented in Go via
// //export, which makes cgo mark them __declspec(dllexport) on Windows. Match
// that attribute here so MinGW doesn't warn about a dllexport-less
// redeclaration (-Wdll-attribute-on-redeclaration).
#ifdef _WIN32
#define GOTK4_EXPORTED __declspec(dllexport)
#else
#define GOTK4_EXPORTED
#endif

extern GOTK4_EXPORTED void goToggleNotify(gpointer, GObject *, gboolean);
extern GOTK4_EXPORTED void goFinishRemovingToggleRef(gpointer);
const gchar *gotk4_object_type_name(gpointer obj);
gboolean gotk4_intern_remove_toggle_ref(gpointer obj);
