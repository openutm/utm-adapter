#pragma once

#include "QGCCorePlugin.h"
#include <QtQml/QQmlAbstractUrlInterceptor>

class OpenUTMInterceptor : public QQmlAbstractUrlInterceptor
{
public:
    QUrl intercept(const QUrl &url, DataType type) override;
};

class OpenUTMPlugin : public QGCCorePlugin
{
public:
    explicit OpenUTMPlugin(QObject *parent = nullptr) : QGCCorePlugin(parent) {}
    static QGCCorePlugin *instance();
    QQmlApplicationEngine *createQmlApplicationEngine(QObject *parent) override;
    void destroyQmlApplicationEngine(QQmlApplicationEngine *engine) override;

private:
    OpenUTMInterceptor _interceptor;
};
