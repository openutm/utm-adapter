#include "OpenUTMPlugin.h"
#include <QtCore/QApplicationStatic>
#include <QtCore/QFile>
#include <QtQml/QQmlApplicationEngine>

Q_APPLICATION_STATIC(OpenUTMPlugin, openUTMPluginInstance);

QGCCorePlugin *OpenUTMPlugin::instance()
{
    return openUTMPluginInstance();
}

QQmlApplicationEngine *OpenUTMPlugin::createQmlApplicationEngine(QObject *parent)
{
    auto *engine = QGCCorePlugin::createQmlApplicationEngine(parent);
    engine->addUrlInterceptor(&_interceptor);
    return engine;
}

void OpenUTMPlugin::destroyQmlApplicationEngine(QQmlApplicationEngine *engine)
{
    if (engine) {
        engine->removeUrlInterceptor(&_interceptor);
    }
    QGCCorePlugin::destroyQmlApplicationEngine(engine);
}

QUrl OpenUTMInterceptor::intercept(const QUrl &url, DataType type)
{
    if ((type == QmlFile || type == UrlString) && url.scheme() == QStringLiteral("qrc")) {
        const QString path = QStringLiteral("/Custom") + url.path();
        if (QFile::exists(QStringLiteral(":") + path)) {
            QUrl replacement;
            replacement.setScheme(QStringLiteral("qrc"));
            replacement.setPath(path);
            return replacement;
        }
    }
    return url;
}
