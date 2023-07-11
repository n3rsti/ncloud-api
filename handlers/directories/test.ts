import { Component, Input } from '@angular/core';
import { Subject } from 'rxjs';
import { ToastInput } from 'src/app/interfaces';

@Component({
    selector: 'app-toast',
    templateUrl: './toast.component.html',
    styleUrls: ['./toast.component.scss'],
})
export class ToastComponent {
    @Input() data: Subject<any> = new Subject<any>();

    opened: boolean = true;
    icon: string = '';
    message: string = '';

    icons = {
        check:
            'M16.707 5.293a1 1 0 010 1.414l-8 8a1 1 0 01-1.414 0l-4-4a1 1 0 011.414-1.414L8 12.586l7.293-7.293a1 1 0 011.414 0z',
        error:
            'M4.293 4.293a1 1 0 011.414 0L10 8.586l4.293-4.293a1 1 0 111.414 1.414L11.414 10l4.293 4.293a1 1 0 01-1.414 1.414L10 11.414l-4.293 4.293a1 1 0 01-1.414-1.414L8.586 10 4.293 5.707a1 1 0 010-1.414z',
        warning: '',
    };

    ngOnInit() {
        this.data.subscribe((data: ToastInput) => {
            this.icon = data.icon;
            this.message = data.message;
            this.opened = true;
        });
    }
}

