# Kolejność materializacji rozszerzeń

## Decyzja

`Bundle.MaterializePhase` deklaruje fazę materializacji rozszerzenia. Runner wykonuje najpierw `compiler.MaterializeDeclarations`, potem `compiler.MaterializeCompositions`. W obrębie fazy zachowuje kolejność przekazanych bundli. Wartość zerowa oznacza deklaracje, więc istniejące rozszerzenia nie muszą zmieniać swoich deklaracji. Nieobsługiwana faza jest błędem walidacji bundle, również gdy bundle nie ma materializera.

Faza deklaracji montuje akcje na podstawie metadanych. Faza kompozycji kompiluje deklaracje odwołujące się do tych akcji. Rozszerzenie `pipeline` samo deklaruje fazę kompozycji; runner nie rozpoznaje jego identyfikatora. Kolejność nie zależy od `Bundle.Alias`, a przekazanie aliasu do materializera pozostaje bez zmian. Bundle bez materializera nie dodaje kroku wykonania.

## Dlaczego ten kontrakt

Dotychczas runner przesuwał bundle o identyfikatorze `pipeline` na koniec. Była to wiedza o konkretnej funkcji języka w warstwie wykonawczej. Sam adapter rozszerzenia nie może zapewnić kolejności wobec materializerów innych bundli: ustala ją `runner.BuildConfig`.

Kontrakt pozostaje w istniejącym mikrordzeniu rozszerzeń Flow (`compiler.Bundle`), a runner jedynie go wykonuje. Nie dodajemy mechanizmu do zewnętrznego modułu Kernel: to kolejność etapów kompilacji Flow, a nie nowa prymitywa wykonania akcji. Sprawdzono istniejące kontrakty Bundle/Materializer i katalog reuse; DAG Kernela służy grafom akcji, nie jest potrzebny do stabilnego uporządkowania dwóch faz.

Alternatywy: warunek po nazwie narusza granicę rozszerzeń; ręczna kolejność natywnego katalogu nie obejmuje zewnętrznych bundli; dowolne priorytety liczbowe nie opisują intencji; graf zależności przed/po byłby większym kontraktem niż potrzebuje ten etap. Używamy stabilnego sortowania z biblioteki standardowej, bez własnego sortowania topologicznego.

## Granice i zgodność

To kontrakt faz, nie graf zależności między rozszerzeniami i nie gwarancja rozwiązania zależności wewnątrz jednej fazy. Wewnętrzne zależności sub-pipelines nadal rozwiązuje rozszerzenie `pipeline`. Kolejność dyrektyw, hooków, bibliotek, preprocessingu i wrapperów nie zmienia się. Nie zmieniamy sygnatury `Materializer` ani struktury `MaterializeReq`.

Dodanie pola do publicznego `Bundle` zachowuje istniejące literały z nazwami pól; zewnętrzne literały pozycyjne wymagają aktualizacji. To ograniczenie zgodności źródłowej należy uwzględnić przy wydaniu. Nowe wartości faz wymagają jawnego rozszerzenia kontraktu i jego walidacji.

## Weryfikacja

Testy obejmują stabilność faz, neutralność identyfikatorów (także nazwy `pipeline`), zachowanie aliasów i żądania, brak mutacji wejściowych bundli, puste materializery, propagację błędów, walidację faz i integrację rzeczywistego pipeline z deklaracją dostarczoną przez inny bundle. Istniejące testy zależności i cykli sub-pipelines pozostają aktywne.
