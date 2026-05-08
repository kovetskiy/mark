<!-- Space: IT -->
<!-- Parent: IT инфраструктура -->
<!-- Parent: ИС -->
<!-- Parent: ESB & DLH -->

# Процесс заселения в Glorax [07.05.26]

## 1. Схема процесса

<!-- Macro: ```mermaid-cloud\n([\s\S]*?)```
     Template: #mermaid-cloud
     mermaid-cloud: |
       <ac:structured-macro ac:name="mermaid-cloud" ac:schema-version="1">
       <ac:parameter ac:name="toolbar">bottom</ac:parameter>
       <ac:parameter ac:name="format">svg</ac:parameter>
       <ac:parameter ac:name="zoom">fit</ac:parameter>
       <ac:plain-text-body><![CDATA[${1}]]></ac:plain-text-body>
       </ac:structured-macro> -->

```mermaid
sequenceDiagram

    participant PR as PlanRadar
    participant XLS as Excel<br>Google sheet
    participant CRM as Dynamics CRM
    participant CC as Колл-центр / ЛК
    participant Client as Клиент
    participant УК as Управляющая компания

    Note over PR: За 3 месяца до РНВ — Подготовка
    PR->>PR: Дефектовка помещений<br>проектной группой

    Note over XLS, Client: Получение РНВ

    par Клиентский сервис
        CRM->>XLS: Формирование реестра
        XLS->>CC: Получение реестра жильцов
        CC->>Client: Отправка уведомлений почтой России
        CC->>Client: СМС-уведомление
        CC->>Client: Телефония
    end

    Client->>CC: Запись на приёмку (ЛК / колл-центр)

    Note over PR, Client: Приёмка на объекте

    alt Стандартный путь
        PR<<->>Client: Акт осмотра (дефекты: текст + фото)
        CRM<<->>Client: Акт приёма-передачи (скан)
    else Жёсткие сроки — клиент не отвечает
        CRM->>Client: Алгоритм «одностороннего акта»
    end

    Note over PR, Client: Выдача ключей
    CRM->>XLS: Бумажный реестр ключей
    Client->>CRM: Подпись клиента

    XLS->>УК: Данные жильцов (Excel)
    УК->>УК: Ручной ввод: домофон, паркинг (Domiline)
```

## 2. Хранение данных и системы

| Данные / процесс | Где хранится / ведётся |
|---|---|
| Дефекты, акты осмотра | **PlanRadar** |
| Статусы готовности, АПП, сканы | **CRM Dynamics** |
| История контактов с клиентом (обзвон, СМС) | **Excel / Google Таблицы** (ручной ввод) |